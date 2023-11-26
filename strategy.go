package main

import (
	// "context"
	// "sync"
	// "encoding/json"
	"log"
	"math"
	"os"

	// "reflect"
	"strconv"
	"strings"
	"time"

	// "code.vegaprotocol.io/quant/interfaces"
	pd "code.vegaprotocol.io/quant/pricedistribution"
	"code.vegaprotocol.io/quant/riskmodelbs"
	"code.vegaprotocol.io/vega/protos/vega"
	vegapb "code.vegaprotocol.io/vega/protos/vega"
	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"

	// walletpb "code.vegaprotocol.io/vega/protos/vega/wallet/v1"
	// "github.com/jeremyletang/vega-go-sdk/wallet"
	"github.com/shopspring/decimal"
	// vegapb "vega-mm/protos/vega"
	// commandspb "vega-mm/protos/vega/commands/v1"
	// walletpb "vega-mm/protos/vega/wallet/v1"
	// "golang.org/x/exp/maps"
)

// Note: Prototype feature that checks taker volume across a short time frame and use it to determine whether
// 		 it is viable to bid at the front of the book. It should be deemed viable when there is adequate and
// 		 frequent buy and sell volume in very short periods of time. This is to ensure that we can earn the
//		 maker fee and immediately offload the position on the other side of the book. Consider adding a
//		 failsafe whereby the strategy will close out the position by crossing the spread if the position is
//		 too large or is held for too long.

// Note: We need to think about what parameters we want to automatically tune to optimize fee revenue, reduce
//		 risk, and prevent adverse selection. We also need to consider how we want to automate/tune these params.
//		 Initially it is likely that the only method within the scope of this project is heuristics based on
//		 market data such as order flow, liquidity, and volatility across short timeframes. There is potential
//		 to experiment with RL and other ML strategies, however, there will be numerous challeneges, eg; data
//		 collection, data formatting, building a training framework, and quantifying model performance during
//		 training, testing, and live operation.
//
//		 Params for probability of trading strategy:
//			- orderSizeBase
//			- maxProbabilityOfTrading
//			- orderSpacing
//			- targetVolume (bidvol, askVol)
//

type agent struct {
	// An agent will control one wallet and can run multiple strategies
	// Note: For the time being an agent will only allow one strategy per market.
	pubkey     string
	apiToken   string
	balance    decimal.Decimal
	strategies map[string]*strategy
	vegaClient *VegaClient
	// walletClient *wallet.Client
	signer    *signer
	config    *Config
	metricsCh chan *MetricsState
}

type Agent interface {
	// Registers a strategy with the agent
	RegisterStrategy(*strategy)

	VegaClient() *VegaClient

	// Loads the decimal data for each registered strategy
	LoadDecimals()

	// Checks current state of Liquidity Commitment, submits one if it does not exist,
	// amends it if there are any changes, or does nothing if there are no changes.
	UpdateLiquidityCommitment(*strategy)

	// Runs the provided strategy
	RunStrategy(*strategy, chan *MetricsState)
}

type StrategyOpts struct {
	marketId                string
	targetObligationVolume  int64
	maxProbabilityOfTrading float64
	orderSpacing            float64
	orderSizeBase           float64
	targetVolCoefficient    float64
	numOrdersPerSide        int
}

type strategy struct {
	marketId                string
	d                       *decimals
	targetObligationVolume  decimal.Decimal
	maxProbabilityOfTrading decimal.Decimal
	orderSpacing            decimal.Decimal
	orderSizeBase           decimal.Decimal
	targetVolCoefficient    decimal.Decimal
	numOrdersPerSide        int

	vegaStore *VegaStore

	agent *agent
}

// Shelve for now but implement later with more strategy-level metrics
type StrategyMetrics struct {
	position              *vegapb.Position
	signedExposure        decimal.Decimal
	pubkeyBalance         decimal.Decimal
	ourBestBid            decimal.Decimal
	ourBestAsk            decimal.Decimal
	vegaBestBid           decimal.Decimal
	vegaBestAsk           decimal.Decimal
	LiveOrdersCount       int
	MarketDataUpdateCount int
}

type Strategy interface {
	GetDecimals(*vegapb.Market, *vegapb.Asset) decimals

	SubmitLiquidityCommitment()

	AmendLiquidityCommitment()

	GetOurBestBidAndAsk([]*vegapb.Order) (decimal.Decimal, decimal.Decimal)

	GetOrderSubmission(decimal.Decimal, decimal.Decimal, decimal.Decimal, decimal.Decimal, vegapb.Side, *vegapb.LogNormalRiskModel, *vegapb.LiquiditySLAParameters) []*commandspb.OrderSubmission

	// GetRiskMetricsForMarket(*vegapb.Market) (something... something... something...)

	GetEntryPriceAndVolume() (decimal.Decimal, decimal.Decimal)

	// GetProbabilityOfTradingForPrice() float64
}

type decimals struct {
	positionFactor decimal.Decimal
	priceFactor    decimal.Decimal
	assetFactor    decimal.Decimal
}

func (agent *agent) RegisterStrategy(strat *strategy) {
	// If strategy for market is already registered, return.
	if strategy := agent.strategies[strat.marketId]; strategy != nil {
		log.Printf("Error: Strategy already found for market, ignoring...")
		return
	}
	strat.agent = agent
	agent.strategies[strat.marketId] = strat
	agent.vegaClient.vegaMarkets = append(agent.vegaClient.vegaMarkets, strat.marketId)
	return
}

func (agent *agent) LoadDecimals() {
	for _, marketId := range agent.vegaClient.vegaMarkets {

		strat := agent.strategies[marketId]
		market := strat.vegaStore.GetMarket()
		asset := strat.vegaStore.GetAsset(market.GetTradableInstrument().GetInstrument().GetPerpetual().SettlementAsset)

		strat.d = &decimals{
			positionFactor: decimal.NewFromInt(10).Pow(decimal.NewFromInt(market.PositionDecimalPlaces)),
			priceFactor:    decimal.NewFromInt(10).Pow(decimal.NewFromInt(int64(market.DecimalPlaces))),
			assetFactor:    decimal.NewFromInt(10).Pow(decimal.NewFromInt(int64(asset.Details.Decimals))),
		}
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

	for range time.NewTicker(1000 * time.Millisecond).C {
		log.Printf("Executing strategy...")

		var (
			marketId        = strat.marketId
			market          = strat.vegaStore.GetMarket()
			settlementAsset = market.GetTradableInstrument().GetInstrument().GetPerpetual().GetSettlementAsset()

			liquidityParams        = market.GetLiquiditySlaParams()
			logNormalRiskModel     = market.GetTradableInstrument().GetLogNormalRiskModel()
			liveOrders             = strat.vegaStore.GetOrders()
			vegaBestBid            = decimal.RequireFromString(strat.vegaStore.GetMarketData().GetBestBidPrice())
			vegaBestAsk            = decimal.RequireFromString(strat.vegaStore.GetMarketData().GetBestOfferPrice())
			vegaSpread             = vegaBestAsk.Sub(vegaBestBid)
			ourBestBid, ourBestAsk = strat.GetOurBestBidAndAsk(liveOrders)
			// ourSpread              = ourBestAsk.Sub(ourBestBid)
			openVol, avgEntryPrice = strat.GetEntryPriceAndVolume()
			notionalExposure       = avgEntryPrice.Mul(openVol).Abs()
			signedExposure         = avgEntryPrice.Mul(openVol)
			balance                = strat.GetPubkeyBalance(settlementAsset)
			// bidVol                 = balance.Mul(strat.targetVolCoefficient)
			// askVol                 = balance.Mul(strat.targetVolCoefficient)
			bidVol               = strat.targetObligationVolume.Mul(strat.targetVolCoefficient)
			askVol               = strat.targetObligationVolume.Mul(strat.targetVolCoefficient)
			neutralityThresholds = []float64{0.01, 0.02, 0.03}
			neutralityOffsets    = []float64{0.001, 0.003, 0.005}
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
		log.Printf("Our best bid: %v, Our best ask: %v", ourBestBid, ourBestAsk)
		log.Printf("Vega best bid: %v, Vega best ask: %v", vegaBestBid, vegaBestAsk)
		log.Printf("Vega spread: %v", vegaSpread)
		log.Printf("Balance: %v", balance)
		log.Printf("Bid volume: %v, ask volume: %v", bidVol, askVol)
		log.Printf("Open volume: %v, entry price: %v, notional exposure: %v", openVol, avgEntryPrice, notionalExposure)

		for i, threshold := range neutralityThresholds {
			value := strat.agent.balance.Mul(decimal.NewFromFloat(threshold))
			switch true {
			case signedExposure.GreaterThan(value):
				// Too much long exposure, step back bid
				bidOffset = decimal.NewFromFloat(neutralityOffsets[i])
				// Push ask to front of book

				// Reduce ask order spacing

			case signedExposure.LessThan(value.Neg()):
				// Too much short exposure, step back ask
				askOffset = decimal.NewFromFloat(neutralityOffsets[i])
				// Push bid to front of book

				// Reduce bid order spacing

			}
		}

		log.Printf("bidOffset: %v, askOffset: %v", bidOffset, askOffset)

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
			var side vegapb.Side
			if signedExposure.IsPositive() {
				side = vegapb.Side_SIDE_SELL
				price = vegaBestAsk
			} else {
				side = vegapb.Side_SIDE_BUY
				price = vegaBestBid
			}

			// Build and append neutrality order
			submissions = append(submissions, &commandspb.OrderSubmission{
				MarketId:    strat.marketId,
				Price:       price.BigInt().String(),
				Size:        openVol.Mul(strat.d.positionFactor).Abs().BigInt().Uint64(),
				Side:        side,
				TimeInForce: vegapb.Order_TIME_IN_FORCE_GTT,
				ExpiresAt:   int64(time.Now().UnixNano() + 8*1e9),
				Type:        vegapb.Order_TYPE_LIMIT,
				PostOnly:    true,
				Reference:   "ref",
			})

		}

		cancellations = append(cancellations, &commandspb.OrderCancellation{MarketId: strat.marketId})

		submissions = append(
			submissions,
			append(
				strat.GetOrderSubmission(vegaBestBid, bidOffset, bidVol, bidReductionAmount, vegapb.Side_SIDE_BUY, logNormalRiskModel, liquidityParams),
				strat.GetOrderSubmission(vegaBestAsk, askOffset, askVol, askReductionAmount, vegapb.Side_SIDE_SELL, logNormalRiskModel, liquidityParams)...,
			)...,
		)

		state := &MetricsState{
			MarketId:              marketId,
			Position:              strat.vegaStore.GetPosition(),
			SignedExposure:        signedExposure,
			VegaBestBid:           vegaBestBid,
			OurBestBid:            ourBestBid,
			VegaBestAsk:           vegaBestAsk,
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

		log.Printf("Response: %v", res)

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

// func NewStrategy(opts *StrategyOpts, config *Config) *strategy {
func NewStrategy(opts *StrategyOpts) *strategy {
	return &strategy{
		marketId:                opts.marketId,
		targetObligationVolume:  decimal.NewFromInt(opts.targetObligationVolume),
		maxProbabilityOfTrading: decimal.NewFromFloat(opts.maxProbabilityOfTrading),
		orderSpacing:            decimal.NewFromFloat(opts.orderSpacing),
		orderSizeBase:           decimal.NewFromFloat(opts.orderSizeBase),
		targetVolCoefficient:    decimal.NewFromFloat(opts.targetVolCoefficient),
		numOrdersPerSide:        opts.numOrdersPerSide,
		vegaStore:               newVegaStore(opts.marketId),
	}
}

// func (strat *strategy) AmendLiquidityCommitment(walletClient *wallet.Client) {
func (strat *strategy) AmendLiquidityCommitment() {
	if market := strat.vegaStore.GetMarket(); market != nil {

		stakeToCcyVolume, err := decimal.NewFromString(strat.vegaStore.GetStakeToCcyVolume())
		if err != nil {
			log.Fatalf("Failed to parse stakeToCcyVolume from string: %v", err)
		}

		// Create LP Amendment
		lpAmendment := &commandspb.LiquidityProvisionAmendment{
			MarketId:         strat.marketId,
			CommitmentAmount: strat.targetObligationVolume.Mul(strat.d.assetFactor).Div(stakeToCcyVolume).BigInt().String(), // Divinde by stakeToCcyVolume
			Fee:              "0.0001",
			Reference:        "Opportunities don't happen, you create them.",
		}

		// Build and send tx
		inputData := &commandspb.InputData{
			Command: &commandspb.InputData_LiquidityProvisionAmendment{
				LiquidityProvisionAmendment: lpAmendment,
			},
		}
		tx := strat.agent.signer.BuildTx(strat.agent.pubkey, inputData)

		res := strat.agent.signer.SubmitTx(tx)

		log.Printf("Response: %v", res)

		// // Submit transaction
		// err = walletClient.SendTransaction(
		// 	context.Background(), strat.agent.pubkey, &walletpb.SubmitTransactionRequest{
		// 		Command: &walletpb.SubmitTransactionRequest_LiquidityProvisionAmendment{
		// 			LiquidityProvisionAmendment: lpAmendment,
		// 		},
		// 	},
		// )

		// if err != nil {
		// 	log.Fatalf("Failed to send transaction: failed to amend liquidity commitment: %v", err)
		// }
	}
}

func (strat *strategy) CancelLiquidityCommitment() {

	if market := strat.vegaStore.GetMarket(); market != nil {

		// Cancel LP submission
		lpCancellation := &commandspb.LiquidityProvisionCancellation{
			MarketId: strat.marketId,
		}

		// Build and send tx
		inputData := &commandspb.InputData{
			Command: &commandspb.InputData_LiquidityProvisionCancellation{
				LiquidityProvisionCancellation: lpCancellation,
			},
		}

		tx := strat.agent.signer.BuildTx(strat.agent.pubkey, inputData)

		res := strat.agent.signer.SubmitTx(tx)

		log.Printf("Response: %v", res)

	}

}

// func (strat *strategy) SubmitLiquidityCommitment(walletClient *wallet.Client) {
func (strat *strategy) SubmitLiquidityCommitment() {

	if market := strat.vegaStore.GetMarket(); market != nil {

		stakeToCcyVolume, err := decimal.NewFromString(strat.vegaStore.GetStakeToCcyVolume())
		if err != nil {
			log.Fatalf("Failed to parse stakeToCcyVolume from string: %v", err)
		}

		// Create LP submission
		lpSubmission := &commandspb.LiquidityProvisionSubmission{
			MarketId:         strat.marketId,
			CommitmentAmount: strat.targetObligationVolume.Mul(strat.d.assetFactor).Div(stakeToCcyVolume).BigInt().String(), // Divide by stakeToCcyVolume to get "commitmentAmount"
			Fee:              "0.0001",
			Reference:        "Opportunities don't happen, you create them.",
		}

		// Build and send tx
		inputData := &commandspb.InputData{
			Command: &commandspb.InputData_LiquidityProvisionSubmission{
				LiquidityProvisionSubmission: lpSubmission,
			},
		}
		tx := strat.agent.signer.BuildTx(strat.agent.pubkey, inputData)

		res := strat.agent.signer.SubmitTx(tx)

		log.Printf("Response: %v", res)

		// // Submit transaction
		// err = walletClient.SendTransaction(
		// 	context.Background(), strat.agent.pubkey, &walletpb.SubmitTransactionRequest{
		// 		Command: &walletpb.SubmitTransactionRequest_LiquidityProvisionSubmission{
		// 			LiquidityProvisionSubmission: lpSubmission,
		// 		},
		// 	},
		// )

		// if err != nil {
		// 	log.Fatalf("Failed to send transaction: failed to submit liquidity commitment: %v", err)
		// }
	}
}

func (strat *strategy) GetOurBestBidAndAsk(liveOrders []*vegapb.Order) (decimal.Decimal, decimal.Decimal) {
	ourBestBid, ourBestAsk := 0, math.MaxInt
	for _, order := range liveOrders {
		if order.Side == vegapb.Side_SIDE_BUY {
			price, err := strconv.Atoi(order.Price)
			if err != nil {
				log.Fatalf("Failed to convert string to int %v", err)
			}
			if price > ourBestBid {
				ourBestBid = price
			}
		} else if order.Side == vegapb.Side_SIDE_SELL {
			price, err := strconv.Atoi(order.Price)
			if err != nil {
				log.Fatalf("Failed to convert string to int %v", err)
			}
			if price < ourBestAsk {
				ourBestAsk = price
			}
		}
	}
	return decimal.NewFromInt(int64(ourBestBid)).Div(strat.d.priceFactor), decimal.NewFromInt(int64(ourBestAsk)).Div(strat.d.priceFactor)
}

func (strat *strategy) GetEntryPriceAndVolume() (volume, entryPrice decimal.Decimal) {

	if strat.vegaStore.GetPosition() == nil {
		return
	}

	volume = decimal.NewFromInt(strat.vegaStore.GetPosition().GetOpenVolume())
	entryPrice, _ = decimal.NewFromString(strat.vegaStore.GetPosition().GetAverageEntryPrice())

	return volume.Div(strat.d.positionFactor), entryPrice.Div(strat.d.priceFactor)
}

func (strat *strategy) GetPubkeyBalance(settlementAsset string) (b decimal.Decimal) {

	// marketId := maps.Keys(vega)[0]

	for _, acc := range strat.vegaStore.GetAccounts() {
		if acc.Owner != strat.agent.pubkey || acc.Asset != settlementAsset {
			continue
		}

		balance, _ := decimal.NewFromString(acc.Balance)
		b = b.Add(balance)
	}

	return b.Div(strat.d.assetFactor)
}

func (strat *strategy) GetOrderSubmission(vegaRefPrice, offset, targetVolume, orderReductionAmount decimal.Decimal, side vegapb.Side, logNormalRiskModel *vegapb.LogNormalRiskModel, liquidityParams *vegapb.LiquiditySLAParameters) []*commandspb.OrderSubmission {

	refPrice, _ := vegaRefPrice.Div(strat.d.priceFactor).Float64()
	priceTriggers := strat.vegaStore.GetMarket().GetPriceMonitoringSettings().GetParameters().GetTriggers()
	firstPrice := findPriceByProbabilityOfTrading(strat.maxProbabilityOfTrading, side, refPrice, logNormalRiskModel, priceTriggers)

	log.Printf("Calculated price: %v, Side: %v \n", firstPrice, side)

	riskParams := logNormalRiskModel.GetParams()

	reductionAmount := orderReductionAmount.Div(vegaRefPrice.Div(strat.d.priceFactor))

	totalOrderSizeUnits := (math.Pow(strat.orderSizeBase.InexactFloat64(), float64(strat.numOrdersPerSide+1)) - float64(1)) / (strat.orderSizeBase.InexactFloat64() - float64(1))
	// totalOrderSizeUnits := (math.Pow(float64(2), float64(numOrders+1)) - float64(1)) / float64(2-1)
	orders := []*commandspb.OrderSubmission{}

	sizeF := func(i int) decimal.Decimal {

		size := targetVolume.Div(decimal.NewFromFloat(totalOrderSizeUnits).Mul(vegaRefPrice.Div(strat.d.priceFactor))).Mul(strat.orderSizeBase.Pow(decimal.NewFromInt(int64(i + 1))))
		adjustedSize := decimal.NewFromInt(0)

		// log.Printf("Reduction Amount: %v\n", reductionAmount)
		// log.Printf("size: %v\n", size)

		if size.LessThan(reductionAmount) {
			reductionAmount = decimal.Max(reductionAmount.Sub(size), decimal.NewFromInt(0))
		} else if size.GreaterThan(reductionAmount) {
			adjustedSize = size.Sub(reductionAmount)
			reductionAmount = decimal.NewFromInt(0)
		} else {
			reductionAmount = decimal.NewFromInt(0)
		}

		return decimal.Max(
			adjustedSize,
			decimal.NewFromInt(1).Div(strat.d.positionFactor),
		)
	}

	// sizeF := func() decimal.Decimal {
	// 	return decimal.Max(targetVolume.Div(decimal.NewFromInt(int64(numOrders)).Mul(vegaRefPrice.Div(d.priceFactor))), decimal.NewFromInt(1).Div(d.positionFactor))
	// }

	log.Printf("targetVol: %v, vegaRefPrice: %v", targetVolume, vegaRefPrice)

	priceF := func(i int) decimal.Decimal {

		// We need to add a clamp so that the min/max price we quote will always be within the valid LP price range.

		// if i == 1 && offset.IsZero() {
		// 	return decimal.NewFromFloat(firstPrice)
		// }
		return decimal.NewFromFloat(firstPrice).Mul(decimal.NewFromInt(1).Sub(decimal.NewFromInt(int64(i)).Mul(strat.orderSpacing)).Sub(offset))

	}

	if side == vegapb.Side_SIDE_SELL {

		priceF = func(i int) decimal.Decimal {
			// if i == 1 && offset.IsZero() {
			// 	return decimal.NewFromFloat(firstPrice)
			// }
			return decimal.NewFromFloat(firstPrice).Mul(decimal.NewFromInt(1).Add(decimal.NewFromInt(int64(i)).Mul(strat.orderSpacing)).Add(offset))
		}

	}

	tau := logNormalRiskModel.GetTau()
	tauScaled := tau * 10
	modelParams := riskmodelbs.ModelParamsBS{
		Mu:    riskParams.Mu,
		R:     riskParams.R,
		Sigma: riskParams.Sigma,
	}
	distS := modelParams.GetProbabilityDistribution(refPrice, tauScaled)

	// bestAsk := decimal.RequireFromString(strat.vegaStore.GetMarketData().GetBestOfferPrice()).Div(strat.d.priceFactor)
	// maxAsk := bestAsk.InexactFloat64() * 6.0

	sumSize := 0.0
	sumSizeMulProb := 0.0

	for i := 0; i < strat.numOrdersPerSide; i++ {

		price := priceF(i)
		size := sizeF(i)

		orders = append(orders, &commandspb.OrderSubmission{
			MarketId:    strat.marketId,
			Price:       price.Mul(strat.d.priceFactor).BigInt().String(),
			Size:        size.Mul(strat.d.positionFactor).BigInt().Uint64(),
			Side:        side,
			TimeInForce: vegapb.Order_TIME_IN_FORCE_GTT,
			ExpiresAt:   int64(time.Now().UnixNano() + 8*1e9),
			Type:        vegapb.Order_TYPE_LIMIT,
			PostOnly:    true,
			Reference:   "ref",
		})

		var isBid bool
		var prob float64
		if side == vegapb.Side_SIDE_BUY {
			isBid = true
			prob = pd.ProbabilityOfTrading(distS, price.InexactFloat64(), isBid, true, 0.0, refPrice)
		} else {
			isBid = false
			prob = pd.ProbabilityOfTrading(distS, price.InexactFloat64(), isBid, true, refPrice, refPrice*6)
		}

		sumSize += size.InexactFloat64()
		sumSizeMulProb += size.InexactFloat64() * prob
		log.Printf("price: %v, size: %v, probability: %v\n", price.InexactFloat64(), size.InexactFloat64(), prob)
	}

	volumeWeightedLiqScore := sumSizeMulProb / sumSize
	log.Printf("Side: %v, volume weighted Liquidity Score: %v", side, volumeWeightedLiqScore)

	// // for i := 1; i <= strat.numOrdersPerSide; i++ {
	// for i := 0; i < strat.numOrdersPerSide; i++ {
	// 	orders = append(orders, &commandspb.OrderSubmission{
	// 		MarketId:    strat.marketId,
	// 		Price:       priceF(i).Mul(strat.d.priceFactor).BigInt().String(),
	// 		Size:        sizeF(i).Mul(strat.d.positionFactor).BigInt().Uint64(),
	// 		Side:        side,
	// 		TimeInForce: vegapb.Order_TIME_IN_FORCE_GTT,
	// 		ExpiresAt:   int64(time.Now().UnixNano() + 8*1e9),
	// 		Type:        vegapb.Order_TYPE_LIMIT,
	// 		PostOnly:    true,
	// 		Reference:   "ref",
	// 	})
	// }

	return orders
}

func findPriceByProbabilityOfTrading(probability decimal.Decimal, side vegapb.Side, refPrice float64, logNormalRiskModel *vegapb.LogNormalRiskModel, priceTriggers []*vegapb.PriceMonitoringTrigger) (price float64) {

	tau := logNormalRiskModel.GetTau()
	tauScaled := tau * 10
	riskParams := logNormalRiskModel.GetParams()
	modelParams := riskmodelbs.ModelParamsBS{
		Mu:    riskParams.Mu,
		R:     riskParams.R,
		Sigma: riskParams.Sigma,
	}
	distS := modelParams.GetProbabilityDistribution(refPrice, tauScaled)

	log.Printf("desiredMaxProbability: %v, refPrice: %v, tauScaled: %v", probability, refPrice, tauScaled)

	// Get price range from distribution
	var minPrice float64
	var maxPrice = math.MaxFloat64
	for _, trigger := range priceTriggers {

		yFrac := float64(trigger.Horizon) / float64(31536000)

		// Get analytical distribution
		dist := modelParams.GetProbabilityDistribution(refPrice, yFrac)

		minP, maxP := pd.PriceRange(dist, decimal.RequireFromString(trigger.Probability).InexactFloat64())

		if minP > minPrice {
			minPrice = minP
		}
		if maxP < maxPrice {
			maxPrice = maxP
		}

	}

	log.Printf("minPrice: %v, maxPrice: %v", minPrice, maxPrice)
	log.Printf("downFactor: %v upFactor: %v", minPrice/refPrice, maxPrice/refPrice)
	// log.Printf("minPrice1: %v, maxPrice1: %v", minPrice1, maxPrice1)
	// log.Printf("minPrice2: %v, maxPrice2: %v", minPrice2, maxPrice2)

	price = refPrice
	calculatedProb := float64(1)
	// Iterate through prices until we find a price that corresponds to our desired probability.
	for calculatedProb > probability.InexactFloat64() {
		if side == vegapb.Side_SIDE_BUY {
			// price = price - float64(price*0.0002)
			price = price - (refPrice * 0.001)
		} else {
			// price = price + float64(price*0.0002)
			price = price + (refPrice * 0.001)
		}

		var isBid bool
		if side == vegapb.Side_SIDE_BUY {
			isBid = true
			calculatedProb = pd.ProbabilityOfTrading(distS, price, isBid, true, 0.0, refPrice)
		} else {
			isBid = false
			calculatedProb = pd.ProbabilityOfTrading(distS, price, isBid, true, refPrice, refPrice*6)
		}

		// calculatedProb = getProbabilityOfTradingForOrder(riskParams.Mu, riskParams.Sigma, tauScaled, minPrice, maxPrice, price, refPrice, side)

		log.Printf("Side: %v, Price: %v, Last calculated probability: %v, maxProbability: %v", side, price, calculatedProb, probability)
	}

	// log.Printf("Side: %v, Price: %v, Last calculated probability: %v, maxProbability: %v", side, price, calculatedProb, probability)

	return price
}

func getProbabilityOfTradingForOrder(mu, sigma, tau, minPrice, maxPrice, price, bestPrice float64, side vegapb.Side) (probability float64) {

	stddev := sigma * math.Sqrt(tau)

	// log.Printf("stddev: %v", stddev)

	m := math.Log(bestPrice) + (mu-0.5*sigma*sigma)*tau

	// log.Printf("m: %v", m)

	var upperBound float64
	var lowerBound float64
	if side == vegapb.Side_SIDE_BUY {
		upperBound = bestPrice
		lowerBound = minPrice
	} else {
		upperBound = maxPrice
		lowerBound = bestPrice
	}

	if price < lowerBound || price > upperBound {
		return 0
	}

	min := cdf(m, stddev, lowerBound)
	max := cdf(m, stddev, upperBound)
	z := max - min

	// log.Printf("min: %v", min)
	// log.Printf("max: %v", max)
	// log.Printf("z: %v", z)

	if side == vegapb.Side_SIDE_BUY {
		return (cdf(m, stddev, price) - min) / z
	}
	return (max - cdf(m, stddev, price)) / z
}

func cdf(m, stddev, x float64) float64 {

	// log.Printf("m: %v, stddev: %v, x: %v", m, stddev, x)
	// log.Printf("result: %v", (0.5 * math.Erfc(-(math.Log(x)-m)/math.Sqrt(2.0)*stddev)))

	return 0.5 * math.Erfc(-(math.Log(x)-m)/math.Sqrt(2.0)*stddev)
}
