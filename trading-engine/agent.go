package trading

import (
	"log"
	"vega-mm/pow"
	strats "vega-mm/strategies"
	"vega-mm/wallets"

	"github.com/shopspring/decimal"
	"golang.org/x/exp/maps"
)

type Agent struct {
	pubkey     string
	apiToken   string
	balance    decimal.Decimal
	strategies map[string]strats.Strategy
	powStore   *pow.PowStore
	signer     *wallets.VegaSigner
	config     *Config
	metricsCh  chan *MetricsState
}

// Adds the strategy to the agents strategy map
func (a *Agent) RegisterStrategy(strat strats.Strategy) {

	vegaMarket := strat.GetVegaMarket()

	if _, ok := a.strategies[vegaMarket]; ok {
		log.Printf("Strategy already registered for vega market. Ignoring...")
		return
	}

	a.strategies[vegaMarket] = strat
}

// Loads the vega decimals for each strategy. Must be called after connecting to
// and receiving initial data from a data-node.
func (a *Agent) LoadVegaDecimals() {
	for _, strat := range maps.Values(a.strategies) {
		market := strat.GetVegaStore().GetMarket()

		asset := strat.GetVegaStore().GetAsset(market.GetTradableInstrument().GetInstrument().GetPerpetual().SettlementAsset)

		strat.SetDecimals(&strats.Decimals{
			positionFactor: decimal.NewFromInt(10).Pow(decimal.NewFromInt(market.PositionDecimalPlaces)),
			priceFactor:    decimal.NewFromInt(10).Pow(decimal.NewFromInt(int64(market.DecimalPlaces))),
			assetFactor:    decimal.NewFromInt(10).Pow(decimal.NewFromInt(int64(asset.Details.Decimals))),
		})
	}
}

func (agent *agent) VegaClient() *VegaClient {
	return agent.vegaClient
}

func (agent *agent) UpdateLiquidityCommitment(strat *strategy) {
	lpCommitment := strat.vegaStore.GetLiquidityProvision()

	// Steps to more accurately check the state of our commitment:
	// 	- Get liquidityProviderSLA information
	// 	- Compare required_liquidity to strategyOpts.targetObligationVolume

	switch true {
	case (lpCommitment != nil && strat.targetObligationVolume.IsZero()):
		strat.CancelLiquidityCommitment()
	case (lpCommitment == nil && !strat.targetObligationVolume.IsZero()):
		strat.SubmitLiquidityCommitment()
	case (lpCommitment != nil && !strat.targetObligationVolume.IsZero()):
		strat.AmendLiquidityCommitment()
	default:
		return
	}

	// switch true {
	// case (lpCommitment != nil && strat.targetObligationVolume.IsZero()):
	// 	// strat.CancelLiquidityCommitment(walletClient)
	// case (lpCommitment == nil && !strat.targetObligationVolume.IsZero()):
	// 	strat.SubmitLiquidityCommitment(agent.walletClient)
	// case (lpCommitment != nil && !strat.targetObligationVolume.IsZero()):
	// 	strat.AmendLiquidityCommitment(agent.walletClient)
	// default:
	// 	return
	// }
}

func (a *agent) RunStrategy(strat *strategy, metricsCh chan *MetricsState) {
	// Currently, for this setup, the agent has one set of strategy logic and each "strategy" we register
	// with it is just a different set of parameters for the same strategy logic. This is fine when we just
	// want to run a simple strategy to earn LP fees on multiple markets but if we want to run more
	// complex strategies that have distinct logic from one another then we need to refactor this to call
	// a "Run" method on a strategy interface. Then each strategy can have it's own set of business logic.

	for range time.NewTicker(750 * time.Millisecond).C {
		log.Printf("Executing strategy for %v...", strat.binanceMarket)

		var (
			marketId        = strat.marketId
			market          = strat.vegaStore.GetMarket()
			settlementAsset = market.GetTradableInstrument().GetInstrument().GetPerpetual().GetSettlementAsset()

			liquidityParams    = market.GetLiquiditySlaParams()
			logNormalRiskModel = market.GetTradableInstrument().GetLogNormalRiskModel()
			liveOrders         = strat.vegaStore.GetOrders()
			marketData         = strat.vegaStore.GetMarketData()
			vegaBestBid        = decimal.RequireFromString(marketData.GetBestBidPrice())
			vegaBestAsk        = decimal.RequireFromString(marketData.GetBestOfferPrice())
			vegaMidPrice       = decimal.RequireFromString(marketData.GetMidPrice())
			// vegaSpread             = vegaBestAsk.Sub(vegaBestBid)
			ourBestBid, ourBestAsk = strat.GetOurBestBidAndAsk(liveOrders)
			binanceBestBid         = strat.binanceStore.GetBestBid()
			binanceBestAsk         = strat.binanceStore.GetBestAsk()
			openVol, avgEntryPrice = strat.GetEntryPriceAndVolume()
			// notionalExposure       = avgEntryPrice.Mul(openVol).Abs()
			signedExposure = avgEntryPrice.Mul(openVol)
			balance        = strat.GetPubkeyBalance(settlementAsset)
			// bidVol                 = balance.Mul(strat.targetVolCoefficient)
			// askVol                 = balance.Mul(strat.targetVolCoefficient)
			bidVol = strat.targetObligationVolume.Mul(strat.targetVolCoefficient)
			askVol = strat.targetObligationVolume.Mul(strat.targetVolCoefficient)
			// neutralityThresholds = []float64{0.005, 0.01, 0.02}
			neutralityThresholds = []float64{0, 0.01, 0.035, 0.075}
			neutralityOffsets    = []float64{0.0005, 0.00075, 0.00125, 0.002}
			bidReductionAmount   = decimal.Max(signedExposure, decimal.NewFromInt(0))
			askReductionAmount   = decimal.Min(signedExposure, decimal.NewFromInt(0)).Abs()
			bidOffset            = decimal.NewFromInt(0)
			askOffset            = decimal.NewFromInt(0)
			// bidOrderSpacing        = decimal.NewFromFloat(0.001)
			// askOrderSpacing        = decimal.NewFromFloat(0.001)

			submissions   = []*commandspb.OrderSubmission{}
			cancellations = []*commandspb.OrderCancellation{}
		)

		strat.agent.balance = balance

		log.Println("numLiveOrders: ", len(liveOrders))
		// log.Printf("Our best bid: %v, Our best ask: %v", ourBestBid, ourBestAsk)
		// log.Printf("Vega best bid: %v, Vega best ask: %v", vegaBestBid, vegaBestAsk)
		// log.Printf("Vega spread: %v", vegaSpread)
		// log.Printf("Balance: %v", balance)
		// log.Printf("Bid volume: %v, ask volume: %v", bidVol, askVol)
		// log.Printf("Open volume: %v, entry price: %v, notional exposure: %v", openVol, avgEntryPrice, notionalExposure)

		// Let's refactor this part to allow us to define a bias

		for i, threshold := range neutralityThresholds {
			// _ = i
			// _ = neutralityOffsets
			value := strat.agent.balance.Mul(decimal.NewFromFloat(threshold))
			switch true {
			case signedExposure.GreaterThan(value):
				// Too much long exposure, step back bid
				bidOffset = decimal.NewFromFloat(neutralityOffsets[i])
				// Push asks forward
				// askOffset = decimal.NewFromFloat(neutralityOffsets[i]).Neg()

				// Reduce ask order spacing

			case signedExposure.LessThan(value.Neg()):
				// Too much short exposure, step back ask
				askOffset = decimal.NewFromFloat(neutralityOffsets[i])
				// Push bids forward
				// bidOffset = decimal.NewFromFloat(neutralityOffsets[i]).Neg()

				// Reduce bid order spacing

			}
		}

		log.Printf("bidOffset: %v, askOffset: %v", bidOffset, askOffset)

		// One method to stay neutral could be to increase the offset and the order spacing when volatility
		// is high, we could measure the short term volatility by sampling the best bid/best ask over time
		// and calculating short term volatility from the mid price between these samples.
		//	- Consider splitting movements into upside and downside volatility to add directionality.
		_ = &vegapb.MarketData{}

		// We want to stay neutral so we place an order at the best bid or best ask with size equal to our
		// exposure, this will close out our exposure as soon as possible.
		// Note: If the price moves very fast one way and we get filled, this method will offload the exposure
		// 		 as fast as possible but at the expense of getting poor execution because we push to the front
		//		 of the book.
		//
		// 		 We could later on make this a set of orders with a martingale distribution over a tight price
		//		 range very close to the best order. This could make it slower to close out but may give us better
		//		 execution, the main risk would then be that the market would move against us. Alternatively
		//		 we could just push our existing orders on that side to the front of the book by modifying the
		//		 offset for that side.
		//
		//		 Another alternative is to simply keep the exposure and hedge it on a different market elsewhere
		//		 either on another market on Vega or on a centralized exchange with low fees eg; Binance
		//		 In this scenario we would want to closely track our PnLs on both exchanges and compare the total
		//		 PnL to alternative strategies. We would also want to periodically flatten our inventory on both
		//		 exchanges so that we are using our margin efficiently and not risking running out of margin.
		if !openVol.IsZero() {

			var price decimal.Decimal
			var binancePrice decimal.Decimal
			var side vegapb.Side
			if signedExposure.IsPositive() {
				side = vegapb.Side_SIDE_SELL
				price = vegaBestAsk
				binancePrice = binanceBestAsk
			} else {
				side = vegapb.Side_SIDE_BUY
				price = vegaBestBid
				binancePrice = binanceBestBid
			}

			_ = price
			_ = binancePrice
			_ = side
			// Build and append neutrality order
			// We need to refactor this to use a separate martingale distribution near the front of the book instead
			// of one big order right at the front. Either that or disable this feature completely and use other neutrality
			// strategies.
			submissions = append(submissions, &commandspb.OrderSubmission{
				MarketId: strat.marketId,
				// Price:       price.BigInt().String(),
				Price:       binancePrice.Mul(strat.d.priceFactor).BigInt().String(),
				Size:        openVol.Mul(strat.d.positionFactor).Abs().BigInt().Uint64(),
				Side:        side,
				TimeInForce: vegapb.Order_TIME_IN_FORCE_GTT,
				ExpiresAt:   int64(time.Now().UnixNano() + 10*1e9),
				Type:        vegapb.Order_TYPE_LIMIT,
				PostOnly:    true,
				Reference:   "ref",
			})

		}

		cancellations = append(cancellations, &commandspb.OrderCancellation{MarketId: strat.marketId})

		submissions = append(
			submissions,
			append(
				strat.GetOrderSubmission(binanceBestBid, vegaBestBid, vegaMidPrice, bidOffset, bidVol, bidReductionAmount, vegapb.Side_SIDE_BUY, logNormalRiskModel, liquidityParams),
				strat.GetOrderSubmission(binanceBestAsk, vegaBestAsk, vegaMidPrice, askOffset, askVol, askReductionAmount, vegapb.Side_SIDE_SELL, logNormalRiskModel, liquidityParams)...,
			)...,
		)

		state := &MetricsState{
			MarketId:              marketId,
			Position:              strat.vegaStore.GetPosition(),
			SignedExposure:        signedExposure,
			VegaBestBid:           vegaBestBid.Div(strat.d.priceFactor),
			OurBestBid:            ourBestBid,
			VegaBestAsk:           vegaBestAsk.Div(strat.d.priceFactor),
			OurBestAsk:            ourBestAsk,
			LiveOrdersCount:       len(strat.vegaStore.GetOrders()),
			MarketDataUpdateCount: int(strat.vegaStore.marketDataUpdateCounter),
		}

		metricsCh <- state

		batch := commandspb.BatchMarketInstructions{
			Cancellations: cancellations,
			Submissions:   submissions,
		}

		// Build and send tx
		inputData := &commandspb.InputData{
			Command: &commandspb.InputData_BatchMarketInstructions{
				BatchMarketInstructions: &batch,
			},
		}

		// log.Printf("Input Data: %v", inputData)
		// log.Printf("Batch market instructions: %v", inputData.Command.(*commandspb.InputData_BatchMarketInstructions).BatchMarketInstructions)
		// log.Printf("Submissions: %v", inputData.Command.(*commandspb.InputData_BatchMarketInstructions).BatchMarketInstructions.Submissions)

		tx := a.signer.BuildTx(a.pubkey, inputData)

		res := a.signer.SubmitTx(tx)

		log.Printf("%v Tx Response: %v", strat.binanceMarket, res)

		// // Send transaction
		// err := strat.agent.walletClient.SendTransaction(
		// 	context.Background(), strat.agent.pubkey, &walletpb.SubmitTransactionRequest{
		// 		Command: &walletpb.SubmitTransactionRequest_BatchMarketInstructions{
		// 			BatchMarketInstructions: &batch,
		// 		},
		// 	},
		// )

		// if err != nil {
		// 	log.Printf("Error subitting the batch: %v", err)
		// }

	}

	return
}

func (a *agent) StartMetrics() chan *MetricsState {
	return make(chan *MetricsState)
}

// func NewAgent(walletClient *wallet.Client, config *Config) Agent {
func NewAgent(wallet *embeddedWallet, config *Config) Agent {
	agent := &agent{
		pubkey:     config.WalletPubkey,
		strategies: map[string]*strategy{},
		vegaClient: &VegaClient{
			grpcAddresses: strings.Split(config.VegaGrpcAddresses, ","),
			vegaMarkets:   []string{}, // strings.Split(config.VegaMarkets, ","),
			reconnChan:    make(chan struct{}),
			reconnecting:  false,
		},
		// walletClient: walletClient,
		config:    config,
		metricsCh: make(chan *MetricsState),
	}
	agent.vegaClient.agent = agent
	agent.binanceClient = newBinanceClient(agent, config.BinanceWsAddr)
	agent.signer = newSigner(wallet, strings.Split(config.VegaCoreAddrs, ","), agent).(*signer)
	return agent
}

func (s *strategy) GetDecimals(market *vegapb.Market, asset *vegapb.Asset) {
	s.d = &decimals{
		positionFactor: decimal.NewFromInt(10).Pow(decimal.NewFromInt(market.PositionDecimalPlaces)),
		priceFactor:    decimal.NewFromInt(10).Pow(decimal.NewFromInt(int64(market.DecimalPlaces))),
		assetFactor:    decimal.NewFromInt(10).Pow(decimal.NewFromInt(int64(asset.Details.Decimals))),
	}
}
