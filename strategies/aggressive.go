package strats

import (
	"log"
	"math"
	"math/rand"
	"strconv"
	"time"
	"vega-mm/metrics"
	"vega-mm/stores"

	vegapb "code.vegaprotocol.io/vega/protos/vega"
	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"
	"github.com/shopspring/decimal"
)

// type GeneralOpts struct {
// 	AgentKeyPairIdx        uint64
// 	VegaMarketId           string
// 	BinanceMarket          string
// 	NumOrdersPerSide       int
// 	LiquidityCommitment    bool
// 	TargetObligationVolume decimal.Decimal
// 	TargetVolCoefficient   decimal.Decimal
// }

type Aggressive *AggressiveOpts

type AggressiveOpts struct {
	InitialOffset         decimal.Decimal
	ReduceExposureAsTaker bool
	ReductionThreshold    decimal.Decimal
	ReductionFactor       decimal.Decimal
	// ReductionOffset       decimal.Decimal
	HighAggression bool
}

type AggressiveStrategy struct {
	d                  *decimals
	agentPubKey        string
	agentPubKeyBalance syncPubKeyBalance

	*GeneralOpts
	*AggressiveOpts

	vegaStore    *stores.VegaStore
	binanceStore *stores.BinanceStore

	txDataCh chan *commandspb.InputData
}

func NewAggressiveStrategy(opts *StrategyOpts[Aggressive]) *AggressiveStrategy {
	return &AggressiveStrategy{
		GeneralOpts:    opts.General,
		AggressiveOpts: opts.Specific,
		vegaStore:      stores.NewVegaStore(opts.General.VegaMarketId),
		binanceStore:   stores.NewBinanceStore(opts.General.BinanceMarket),
	}
}

func (strat *AggressiveStrategy) SetTxDataChan(ch chan *commandspb.InputData) {
	strat.txDataCh = ch
}

func (strat *AggressiveStrategy) GetAgentKeyPairIndex() uint64 {
	return strat.AgentKeyPairIdx
}

func (strat *AggressiveStrategy) GetVegaMarketId() string {
	return strat.VegaMarketId
}

func (strat *AggressiveStrategy) GetBinanceMarketTicker() string {
	return strat.BinanceMarket
}

func (strat *AggressiveStrategy) GetVegaStore() *stores.VegaStore {
	return strat.vegaStore
}

func (strat *AggressiveStrategy) GetBinanceStore() *stores.BinanceStore {
	return strat.binanceStore
}

func (strat *AggressiveStrategy) GetMarketSettlementAsset() string {
	return strat.GetVegaStore().GetMarket().GetTradableInstrument().GetInstrument().GetPerpetual().GetSettlementAsset()
}

func (strat *AggressiveStrategy) SetVegaDecimals(positionDecimals, priceDecimals, assetDecimals int64) {
	strat.d = &decimals{
		positionFactor: decimal.NewFromInt(10).Pow(decimal.NewFromInt(positionDecimals)),
		priceFactor:    decimal.NewFromInt(10).Pow(decimal.NewFromInt(priceDecimals)),
		assetFactor:    decimal.NewFromInt(10).Pow(decimal.NewFromInt(assetDecimals)),
	}
}

func (strat *AggressiveStrategy) GetVegaDecimals() *decimals {
	return strat.d
}

func (strat *AggressiveStrategy) UsesLiquidityCommitment() bool {
	return strat.LiquidityCommitment
}

func (strat *AggressiveStrategy) GetTargetObligationVolume() decimal.Decimal {
	return strat.TargetObligationVolume
}

func (strat *AggressiveStrategy) SubmitLiquidityCommitment() {

	if market := strat.vegaStore.GetMarket(); market != nil {

		stakeToCcyVolume, err := decimal.NewFromString(strat.vegaStore.GetStakeToCcyVolume())
		if err != nil {
			log.Fatalf("Failed to parse stakeToCcyVolume from string: %v", err)
		}

		// Create LP submission
		lpSubmission := &commandspb.LiquidityProvisionSubmission{
			MarketId:         strat.VegaMarketId,
			CommitmentAmount: strat.TargetObligationVolume.Mul(strat.d.assetFactor).Div(stakeToCcyVolume).BigInt().String(), // Divide by stakeToCcyVolume to get "commitmentAmount"
			Fee:              "0.0001",
			Reference:        "Opportunities don't happen, you create them.",
		}

		// Build and send tx
		inputData := &commandspb.InputData{
			Command: &commandspb.InputData_LiquidityProvisionSubmission{
				LiquidityProvisionSubmission: lpSubmission,
			},
		}

		strat.txDataCh <- inputData

		log.Printf("Market: %v. Sent LP Submission tx data for signing.", strat.BinanceMarket)
	}
}

func (strat *AggressiveStrategy) AmendLiquidityCommitment() {
	if market := strat.vegaStore.GetMarket(); market != nil {

		stakeToCcyVolume, err := decimal.NewFromString(strat.vegaStore.GetStakeToCcyVolume())
		if err != nil {
			log.Fatalf("Failed to parse stakeToCcyVolume from string: %v", err)
		}

		// Create LP Amendment
		lpAmendment := &commandspb.LiquidityProvisionAmendment{
			MarketId:         strat.VegaMarketId,
			CommitmentAmount: strat.TargetObligationVolume.Mul(strat.d.assetFactor).Div(stakeToCcyVolume).BigInt().String(), // Divinde by stakeToCcyVolume
			Fee:              "0.00001",
			Reference:        "Opportunities don't happen, you create them.",
		}

		// Build and send tx
		inputData := &commandspb.InputData{
			Command: &commandspb.InputData_LiquidityProvisionAmendment{
				LiquidityProvisionAmendment: lpAmendment,
			},
		}

		strat.txDataCh <- inputData

		log.Printf("Market: %v. Sent LP Amendment tx data for signing.", strat.BinanceMarket)
	}
}

func (strat *AggressiveStrategy) CancelLiquidityCommitment() {
	if market := strat.vegaStore.GetMarket(); market != nil {

		// Cancel LP submission
		lpCancellation := &commandspb.LiquidityProvisionCancellation{
			MarketId: strat.VegaMarketId,
		}

		// Build and send tx
		inputData := &commandspb.InputData{
			Command: &commandspb.InputData_LiquidityProvisionCancellation{
				LiquidityProvisionCancellation: lpCancellation,
			},
		}

		strat.txDataCh <- inputData

		log.Printf("Market: %v. Sent LP Cancellation tx data for signing.", strat.BinanceMarket)
	}
}

// Locks not used because this should be writen once only.
func (s *AggressiveStrategy) SetAgentPubKey(pubkey string) {
	s.agentPubKey = pubkey
}

// No lock because this value should be written once only.
func (s *AggressiveStrategy) GetAgentPubKey() string {
	return s.agentPubKey
}

func (s *AggressiveStrategy) SetAgentPubKeyBalance(balance decimal.Decimal) {
	s.agentPubKeyBalance.mu.Lock()
	defer s.agentPubKeyBalance.mu.Unlock()
	s.agentPubKeyBalance.value = balance
}

func (s *AggressiveStrategy) GetAgentPubKeyBalance() decimal.Decimal {
	s.agentPubKeyBalance.mu.RLock()
	defer s.agentPubKeyBalance.mu.RUnlock()
	return s.agentPubKeyBalance.value
}

func (strat *AggressiveStrategy) GetOurBestBidAndAsk(liveOrders []*vegapb.Order) (decimal.Decimal, decimal.Decimal) {
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

func (strat *AggressiveStrategy) GetEntryPriceAndVolume() (volume, entryPrice decimal.Decimal) {

	if strat.vegaStore.GetPosition() == nil {
		return
	}

	volume = decimal.NewFromInt(strat.vegaStore.GetPosition().GetOpenVolume())
	entryPrice, _ = decimal.NewFromString(strat.vegaStore.GetPosition().GetAverageEntryPrice())

	return volume.Div(strat.d.positionFactor), entryPrice.Div(strat.d.priceFactor)
}

func (strat *AggressiveStrategy) GetPubkeyBalance(assetId string) (b decimal.Decimal) {

	// marketId := maps.Keys(vega)[0]

	for _, acc := range strat.vegaStore.GetAccounts() {
		if acc.Owner != strat.agentPubKey || acc.Asset != assetId {
			continue
		}

		balance, _ := decimal.NewFromString(acc.Balance)
		b = b.Add(balance)
	}

	assets := strat.vegaStore.GetAllAssets()
	assetDecimals := int64(assets[assetId].GetDetails().GetDecimals())
	assetFactor := decimal.NewFromInt(10).Pow(decimal.NewFromInt(assetDecimals))

	return b.Div(assetFactor)
}

func (s *AggressiveStrategy) GetOrderSubmission() []*commandspb.OrderSubmission {

	orders := []*commandspb.OrderSubmission{}

	return orders
}

func (strat *AggressiveStrategy) RunStrategy(metricsCh chan *metrics.MetricsEvent) {

	// This strategy aims to maximize LP rewards following the reduction of the tau scaling
	// factor via Vega governance. This will be achieved by placing one large order on each
	// side of the book very near the best bid and ask. This will maximize the liquidity score
	// and result in higher LP rewards. Because we will be at the top of the book for most
	// of the time we will match with most of the incoming taker volume, as such we need to
	// reduce our exposure by stepping our orders back and filling the orders of MMs that
	// are quoting further from the mid. We will lose money on the slippage and fees but the
	// LP rewards will more than make up for this early on.

	var reduceExposureThisCycle bool
	exposureReductionChan := make(chan struct{})
	go func() {
		waitingToReduce := false
		for range exposureReductionChan {
			if waitingToReduce {
				continue
			}
			waitingToReduce = true

			go func() {
				<-time.NewTimer(time.Second * time.Duration(5+rand.Intn(15))).C
				log.Printf("Reducing exposure next cycle...")
				reduceExposureThisCycle = true
				waitingToReduce = false
			}()
		}
	}()

	for range time.NewTicker(time.Millisecond * 750).C {
		log.Printf("Executing strategy for %v...", strat.BinanceMarket)

		var (
			marketId = strat.VegaMarketId
			// market   = strat.vegaStore.GetMarket()
			// settlementAsset = market.GetTradableInstrument().GetInstrument().GetPerpetual().GetSettlementAsset()

			// liquidityParams        = market.GetLiquiditySlaParams()
			// logNormalRiskModel     = market.GetTradableInstrument().GetLogNormalRiskModel()
			liveOrders     = strat.vegaStore.GetOrders()
			marketData     = strat.vegaStore.GetMarketData()
			vegaBestBid    = decimal.RequireFromString(marketData.GetBestBidPrice())
			vegaBestAsk    = decimal.RequireFromString(marketData.GetBestOfferPrice())
			vegaBestBidAdj = vegaBestBid.Div(strat.d.priceFactor)
			vegaBestAskAdj = vegaBestAsk.Div(strat.d.priceFactor)
			// vegaMidPrice           = decimal.RequireFromString(marketData.GetMidPrice())
			ourBestBid, ourBestAsk = strat.GetOurBestBidAndAsk(liveOrders)
			binanceBestBid         = strat.binanceStore.GetBestBid()
			binanceBestAsk         = strat.binanceStore.GetBestAsk()
			openVol, avgEntryPrice = strat.GetEntryPriceAndVolume()
			// notionalExposure       = avgEntryPrice.Mul(openVol).Abs()
			signedExposure = avgEntryPrice.Mul(openVol)
			// balance              = strat.GetPubkeyBalance(settlementAsset)
			// bidVol               = strat.TargetObligationVolume.Mul(strat.TargetVolCoefficient)
			// askVol               = strat.TargetObligationVolume.Mul(strat.TargetVolCoefficient)
			neutralityThresholds = []float64{0.1, 0.2, 0.3, 0.4, 0.5}                //, 0.6}
			neutralityOffsets    = []float64{0.0015, 0.0025, 0.0035, 0.0055, 0.0075} //, 0.0095}
			// bidReductionAmount   = decimal.Max(signedExposure, decimal.NewFromInt(0))
			// askReductionAmount   = decimal.Min(signedExposure, decimal.NewFromInt(0)).Abs()
			bidOffset = strat.InitialOffset // decimal.NewFromFloat(0.0005)
			askOffset = strat.InitialOffset // decimal.NewFromFloat(0.0005)

			submissions   = []*commandspb.OrderSubmission{}
			cancellations = []*commandspb.OrderCancellation{}
		)

		// Increase the offset from the best bid/ask when we have exposure.
		for i, threshold := range neutralityThresholds {
			// _ = i
			// _ = neutralityOffsets

			// This is currently using our balance, should it use obligation size instead? (Yes probably)
			// value := strat.GetAgentPubKeyBalance().Mul(decimal.NewFromFloat(threshold))

			value := strat.GetTargetObligationVolume().Mul(decimal.NewFromFloat(threshold))

			switch true {
			case signedExposure.GreaterThan(value):
				// Too much long exposure, step back bid
				if decimal.NewFromFloat(neutralityOffsets[i]).GreaterThan(strat.InitialOffset) {
					bidOffset = decimal.NewFromFloat(neutralityOffsets[i])
				}

				// Push ask forward

			case signedExposure.LessThan(value.Neg()):
				// Too much short exposure, step back ask
				if decimal.NewFromFloat(neutralityOffsets[i]).GreaterThan(strat.InitialOffset) {
					askOffset = decimal.NewFromFloat(neutralityOffsets[i])
				}

				// Push bid forward

			}
		}

		log.Printf("%v: bidOffset: %v, askOffset: %v", strat.BinanceMarket, bidOffset, askOffset)
		log.Printf("%v: BinanceBestBid: %v, BinanceBestAsk: %v", strat.BinanceMarket, binanceBestBid, binanceBestAsk)

		// // If Binance best price is > 5 basis points behind the Vega best price then peg orders relative to the binance
		// // price instead of vega price by adding to the offset.
		// if binanceBestBid.LessThan(vegaBestAskAdj.Mul(decimal.NewFromFloat(0.9995))) && !strat.binanceStore.IsStale() {
		// 	// Instead of changing the ref price we can just add to the offset.
		// 	bidOffset = bidOffset.Add(decimal.NewFromInt(1).Sub(binanceBestBid.Div(vegaBestBidAdj)))
		// } else if binanceBestAsk.GreaterThan(vegaBeFunding in less than `timeWindow` minutesstAskAdj.Mul(decimal.NewFromFloat(1.0005))) && !strat.binanceStore.IsStale() {
		// 	askOffset = askOffset.Add(binanceBestAsk.Div(vegaBestAskAdj).Sub(decimal.NewFromInt(1)))
		// }

		// We also need to take funding into account and add to the offsets so we don't get arb'd.
		// Easiest way for now is just to determine when the funding is going to occur and increase
		// the bid and ask offsets x minutes before funding occurs.

		// perp := market.GetTradableInstrument().GetInstrument().GetPerpetual()
		// timeTriggers := perp.GetDataSourceSpecForSettlementSchedule().GetData().GetInternal().GetTimeTrigger().GetTriggers()
		// initialFundingTime := *timeTriggers[0].Initial // Unix seconds
		// timeWindow := int64(900 + rand.Intn(600))

		// If within `timeWindow` minutes of funding, increase both offsets.
		// if time.Now().Unix()%initialFundingTime >= (initialFundingTime - timeWindow) {
		// 	bidOffset = decimal.NewFromFloat(0.01)
		// 	askOffset = decimal.NewFromFloat(0.01)
		// }

		// By default we want to use the Binance best bid/ask as our reference prices.
		bidRefPrice := binanceBestBid
		askRefPrice := binanceBestAsk

		// If the Binance best bid is greater than the Vega best ask then we bid relative to the vega best ask.
		if binanceBestBid.GreaterThan(vegaBestAskAdj) {
			bidRefPrice = vegaBestAskAdj
		}

		// If the Binance best ask is lower than the Vega best bid then we ask relative to the Vega best bid.
		if binanceBestAsk.LessThan(vegaBestBidAdj) {
			askRefPrice = vegaBestBidAdj
		}

		// We need a rule to set the initial offset further towards the front of the vega book if we are not
		// already at the front of the Vega book.

		// We could modify the rules below to allow us to quote in front of the binance best price but only
		// by a small amount.

		if strat.HighAggression {
			// If the Binance best bid is less than the Vega best bid, we use binance best bid as the ref price and
			// we set the bid offset to 0
			if binanceBestBid.LessThanOrEqual(vegaBestBidAdj.Mul(decimal.NewFromFloat(1.0005))) && signedExposure.LessThanOrEqual(decimal.Zero) {
				bidOffset = decimal.NewFromInt(0)
			}

			// If the Binance best ask is greater than the vega best ask, we use binance best ask as the ref price
			// and we set the ask offset to 0.
			if binanceBestAsk.GreaterThanOrEqual(vegaBestAskAdj.Mul(decimal.NewFromFloat(0.9995))) && signedExposure.GreaterThanOrEqual(decimal.Zero) {
				askOffset = decimal.NewFromInt(0)
			}
		}

		// Gradually reduce exposure over time.
		// if !openVol.IsZero() {
		if strat.ReduceExposureAsTaker && signedExposure.Abs().GreaterThan(strat.ReductionThreshold) {

			var side vegapb.Side
			var price decimal.Decimal
			var binancePrice decimal.Decimal
			_ = binancePrice

			exposureReductionChan <- struct{}{}

			if reduceExposureThisCycle {
				log.Printf("Reducing exposure...\n")
				log.Printf("SignedExposure: %v\n", signedExposure)
				log.Printf("Reducing vegabestbid: %v, vegaBestAsk: %v\n", vegaBestBid, vegaBestAsk)

				// var positionFraction = decimal.NewFromFloat(0.15)
				var positionFraction = strat.ReductionFactor
				var priceMultiplier decimal.Decimal
				if signedExposure.IsPositive() {
					side = vegapb.Side_SIDE_SELL
					price = vegaBestBid
					priceMultiplier = decimal.NewFromFloat(0.99875)
					bidOffset = bidOffset.Add(decimal.NewFromFloat(0.00125))
					bidRefPrice = vegaBestBidAdj
				} else {
					side = vegapb.Side_SIDE_BUY
					price = vegaBestAsk
					priceMultiplier = decimal.NewFromFloat(1.00125)
					askOffset = askOffset.Add(decimal.NewFromFloat(0.00125))
					askRefPrice = vegaBestAskAdj
				}

				if signedExposure.Abs().LessThan(decimal.NewFromInt(500)) {
					positionFraction = decimal.NewFromInt(1)
				}

				submissions = append(submissions, &commandspb.OrderSubmission{
					MarketId:    strat.VegaMarketId,
					Price:       price.Mul(priceMultiplier).BigInt().String(),
					Size:        openVol.Mul(strat.d.positionFactor).Abs().Mul(positionFraction).BigInt().Uint64(),
					Side:        side,
					TimeInForce: vegapb.Order_TIME_IN_FORCE_IOC,
					Type:        vegapb.Order_TYPE_LIMIT,
					ReduceOnly:  true,
					Reference:   "ref",
				})

				log.Printf("submissions: %+v\n", submissions)

				reduceExposureThisCycle = false
			}

		}

		bidPrice := bidRefPrice.Mul(decimal.NewFromInt(1).Sub(bidOffset))
		askPrice := askRefPrice.Mul(decimal.NewFromInt(1).Add(askOffset))

		// If the bidPrice is less than the vegaBestBid and the vegaBestBid is within 10bp of the
		// binanceBestBid then we quote at the vegaBestBid
		// if bidPrice.LessThanOrEqual(vegaBestBidAdj) &&
		// 	vegaBestBidAdj.LessThanOrEqual(binanceBestBid.Mul(decimal.NewFromFloat(1.001))) &&
		// 	signedExposure.LessThanOrEqual(decimal.NewFromInt(250)) {
		// 	bidPrice = vegaBestBidAdj
		// }

		// if askPrice.GreaterThanOrEqual(vegaBestAskAdj) &&
		// 	vegaBestAskAdj.GreaterThanOrEqual(binanceBestAsk.Mul(decimal.NewFromFloat(0.999))) &&
		// 	signedExposure.GreaterThanOrEqual(decimal.NewFromInt(-250)) {
		// 	askPrice = vegaBestAskAdj
		// }

		log.Printf("%v: bidPrice: %v, askPrice: %v", strat.BinanceMarket, bidPrice, askPrice)

		bidSize := strat.TargetObligationVolume.Mul(strat.TargetVolCoefficient).Div(bidRefPrice)
		askSize := strat.TargetObligationVolume.Mul(strat.TargetVolCoefficient).Div(askRefPrice)

		cancellations = append(cancellations, &commandspb.OrderCancellation{MarketId: strat.VegaMarketId})

		submissions = append(
			submissions,
			&commandspb.OrderSubmission{ // Bid
				MarketId:    strat.VegaMarketId,
				Price:       bidPrice.Mul(strat.d.priceFactor).BigInt().String(),
				Size:        bidSize.Mul(strat.d.positionFactor).BigInt().Uint64(),
				Side:        vegapb.Side_SIDE_BUY,
				TimeInForce: vegapb.Order_TIME_IN_FORCE_GTT,
				ExpiresAt:   int64(time.Now().UnixNano() + 10*1e9),
				Type:        vegapb.Order_TYPE_LIMIT,
				PostOnly:    true,
				Reference:   "ref",
			},
			&commandspb.OrderSubmission{ // Ask
				MarketId:    strat.VegaMarketId,
				Price:       askPrice.Mul(strat.d.priceFactor).BigInt().String(),
				Size:        askSize.Mul(strat.d.positionFactor).BigInt().Uint64(),
				Side:        vegapb.Side_SIDE_SELL,
				TimeInForce: vegapb.Order_TIME_IN_FORCE_GTT,
				ExpiresAt:   int64(time.Now().UnixNano() + 10*1e9),
				Type:        vegapb.Order_TYPE_LIMIT,
				PostOnly:    true,
				Reference:   "ref",
			},
		)

		metricsData := &metrics.StrategyMetricsData{
			MarketId:              marketId,
			BinanceTicker:         strat.BinanceMarket,
			AgentPubkey:           strat.GetAgentPubKey(),
			Position:              strat.vegaStore.GetPosition(),
			SignedExposure:        signedExposure,
			VegaBestBid:           vegaBestBid.Div(strat.d.priceFactor),
			OurBestBid:            ourBestBid,
			VegaBestAsk:           vegaBestAsk.Div(strat.d.priceFactor),
			OurBestAsk:            ourBestAsk,
			LiveOrdersCount:       len(strat.vegaStore.GetOrders()),
			MarketDataUpdateCount: int(strat.vegaStore.GetMarketDataUpdateCounter()),
		}

		metricsCh <- &metrics.MetricsEvent{
			Type: metrics.MetricsEventType_Strategy,
			Data: metricsData,
		}

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

		strat.txDataCh <- inputData

	}

}
