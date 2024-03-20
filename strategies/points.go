package strats

import (
	"log"
	"math"
	"math/rand"
	"strconv"
	"time"
	"vega-mm/metrics"

	vegapb "code.vegaprotocol.io/vega/protos/vega"
	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"
	"github.com/shopspring/decimal"
)

type Points *PointsOpts

type PointsOpts struct {
	InitialOffset         decimal.Decimal
	ReduceExposureAsTaker bool
	ReductionThreshold    decimal.Decimal
	ReductionFactor       decimal.Decimal
	// ReductionOffset       decimal.Decimal
	OrderSpacing  decimal.Decimal
	OrderSizeBase decimal.Decimal
}

type PointsStrategy struct {
	*BaseStrategy
	*PointsOpts
	*GeneralOpts
}

func NewPointsStrategy(opts *StrategyOpts[Points]) *PointsStrategy {
	return &PointsStrategy{
		BaseStrategy: NewBaseStrategy(opts.General),
		GeneralOpts:  opts.General,
		PointsOpts:   opts.Specific,
	}
}

// Will return a martingale distributed set of orders for each side of the book.
func (strat *PointsStrategy) GetOrderSubmission(side vegapb.Side, midRefPrice, offset, targetVolume decimal.Decimal) (orders []*commandspb.OrderSubmission) {

	// Sum = a(1-r^n)/(1-r)
	// r: common ratio (a*base)
	// a: 1
	unitlessOrderSizeSum := decimal.NewFromInt(1).Sub(strat.OrderSizeBase.Pow(decimal.NewFromInt(int64(strat.NumOrdersPerSide)))).Div(decimal.NewFromInt(1).Sub(strat.OrderSizeBase))
	volumeFraction := targetVolume.Div(unitlessOrderSizeSum)

	priceF := func(i int) decimal.Decimal {

		// log.Printf("midRefPrice: %s", midRefPrice.String())
		// log.Printf("Order spacing: %s", strat.OrderSpacing.String())
		// log.Printf("Offset: %s", offset.String())

		return midRefPrice.Mul(decimal.NewFromInt(1).Sub(decimal.NewFromInt(int64(i)).Mul(strat.OrderSpacing)).Sub(offset))
	}

	if side == vegapb.Side_SIDE_SELL {
		priceF = func(i int) decimal.Decimal {

			// log.Printf("midRefPrice: %s", midRefPrice.String())
			// log.Printf("Order spacing: %s", strat.OrderSpacing.String())
			// log.Printf("Offset: %s", offset.String())

			return midRefPrice.Mul(decimal.NewFromInt(1).Add(decimal.NewFromInt(int64(i)).Mul(strat.OrderSpacing)).Add(offset))
		}
	}

	// Function applies the exponential distribution to our orders
	// such that their sum is roughly equal to the targetVolume.
	//
	// We use the lowest of our prices for that side to determine
	// the order sizes so that we don't drop below our LP commitment.
	// Basically it causes us to quote a little more than our
	// commitment without having to increase TargetVolCoefficient.
	sizeF := func(i int) decimal.Decimal {

		// For bids, the lowest price is the last price
		// refPrice := priceF(strat.NumOrdersPerSide - 1)
		refPrice := midRefPrice

		unitlessOrderSize := strat.OrderSizeBase.Pow(decimal.NewFromInt(int64(i)))
		size := unitlessOrderSize.Mul(volumeFraction).Div(refPrice)

		// log.Printf("Ref Price: %s", refPrice.String())
		// log.Printf("Unitless Order Size Sum: %s", unitlessOrderSizeSum.String())
		// log.Printf("Unitless Order Size: %s", unitlessOrderSize.String())
		// log.Printf("Target volume: %s", targetVolume.String())
		// log.Printf("Size: %s", size.String())

		return decimal.Max(
			size,
			decimal.NewFromInt(1).Div(strat.d.positionFactor),
		)
	}

	if side == vegapb.Side_SIDE_SELL {
		sizeF = func(i int) decimal.Decimal {

			// For asks, the lowest price is the first price
			// refPrice := priceF(0)
			refPrice := midRefPrice

			unitlessOrderSize := strat.OrderSizeBase.Pow(decimal.NewFromInt(int64(i)))
			size := unitlessOrderSize.Mul(volumeFraction).Div(refPrice)

			// log.Printf("Ref Price: %s", refPrice.String())
			// log.Printf("Unitless Order Size Sum: %s", unitlessOrderSizeSum.String())
			// log.Printf("Unitless Order Size: %s", unitlessOrderSize.String())
			// log.Printf("Target volume: %s", targetVolume.String())
			// log.Printf("Volume Fraction: %s", volumeFraction.String())
			// log.Printf("Size: %s", size.String())

			return decimal.Max(
				size,
				decimal.NewFromInt(1).Div(strat.d.positionFactor),
			)
		}
	}

	for i := 0; i < strat.NumOrdersPerSide; i++ {

		price := priceF(i)
		size := sizeF(i)

		orders = append(orders, &commandspb.OrderSubmission{
			MarketId:    strat.VegaMarketId,
			Price:       price.Mul(strat.d.priceFactor).BigInt().String(),
			Size:        size.Mul(strat.d.positionFactor).BigInt().Uint64(),
			Side:        side,
			TimeInForce: vegapb.Order_TIME_IN_FORCE_GTT,
			ExpiresAt:   int64(time.Now().UnixNano() + 10*1e9),
			Type:        vegapb.Order_TYPE_LIMIT,
			PostOnly:    true,
			Reference:   "ref",
		})

	}

	return orders
}

func (strat *PointsStrategy) RunStrategy(metricsCh chan *metrics.MetricsEvent) {
	// This strategy aims to farm LP rewards on the EigenLayer points market.
	// Since there is no underlying price we can track anywhere we will simply
	// use the vega best bid and ask to determine how we will price our orders.
	//
	// This strategy will re-use some features from the martingale strategy as
	// we don't want to price all our liquidity at one price point. Using a steep
	// martingale distribution will help up take on exposure more gradually and
	// will help us move the front of the book forward so that other LPs earn
	// less rewards while we don't price too aggressively. We could even extend
	// this to have two very small orders near the front of the book.

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
			liveOrders         = strat.vegaStore.GetOrders()
			liveExternalOrders = strat.vegaStore.GetExternalOrders()
			marketData         = strat.vegaStore.GetMarketData()
			vegaBestBid        = decimal.RequireFromString(marketData.GetBestBidPrice())
			vegaBestAsk        = decimal.RequireFromString(marketData.GetBestOfferPrice())
			// vegaBestBidAdj         = vegaBestBid.Div(strat.d.priceFactor)
			// vegaBestAskAdj         = vegaBestAsk.Div(strat.d.priceFactor)
			// vegaMidPrice           = decimal.RequireFromString(marketData.GetMidPrice())
			// vegaMidPriceAdj        = vegaMidPrice.Div(strat.d.priceFactor)
			ourBestBidAdj, ourBestAskAdj = strat.GetOurBestBidAndAsk(liveOrders)
			openVol, avgEntryPrice       = strat.GetEntryPriceAndVolume()
			// notionalExposure       = avgEntryPrice.Mul(openVol).Abs()
			signedExposure = avgEntryPrice.Mul(openVol)
			// balance              = strat.GetPubkeyBalance(settlementAsset)
			// bidVol               = strat.TargetObligationVolume.Mul(strat.TargetVolCoefficient)
			// askVol               = strat.TargetObligationVolume.Mul(strat.TargetVolCoefficient)
			neutralityThresholds = []float64{0.1, 0.2, 0.3, 0.4, 0.5}        //, 0.6}
			neutralityOffsets    = []float64{0.01, 0.025, 0.05, 0.85, 0.125} //, 0.0095}
			// bidReductionAmount   = decimal.Max(signedExposure, decimal.NewFromInt(0))
			// askReductionAmount   = decimal.Min(signedExposure, decimal.NewFromInt(0)).Abs()
			bidOffset = strat.InitialOffset // decimal.NewFromFloat(0.0005)
			askOffset = strat.InitialOffset // decimal.NewFromFloat(0.0005)

			submissions   = []*commandspb.OrderSubmission{}
			cancellations = []*commandspb.OrderCancellation{}
		)

		// Increase the offset from the best bid/ask when we have exposure.
		var (
			additionalBidOffset decimal.Decimal
			additionalAskOffset decimal.Decimal
		)
		for i, threshold := range neutralityThresholds {
			// _ = i
			// _ = neutralityOffsets

			value := strat.GetTargetObligationVolume().Mul(decimal.NewFromFloat(threshold))

			switch true {
			case signedExposure.GreaterThan(value):
				// Too much long exposure, step back bid
				if decimal.NewFromFloat(neutralityOffsets[i]).GreaterThan(strat.InitialOffset) {
					// bidOffset = decimal.NewFromFloat(neutralityOffsets[i])
					additionalBidOffset = decimal.NewFromFloat(neutralityOffsets[i])
				}

				// Push ask forward

			case signedExposure.LessThan(value.Neg()):
				// Too much short exposure, step back ask
				if decimal.NewFromFloat(neutralityOffsets[i]).GreaterThan(strat.InitialOffset) {
					// askOffset = decimal.NewFromFloat(neutralityOffsets[i])
					additionalAskOffset = decimal.NewFromFloat(neutralityOffsets[i])
				}

				// Push bid forward

			}
		}

		bidOffset = bidOffset.Add(additionalBidOffset)
		askOffset = askOffset.Add(additionalAskOffset)

		log.Printf("%v: bidOffset: %v, askOffset: %v", strat.BinanceMarket, bidOffset, askOffset)

		////// TODO: Quantify book thickness //////
		// We might want to quantify the thickness of the book so that we can avoid
		// quoting far infront of the rest of the LPs. This will require a breaking
		// change to the vega_data_client and it's GetOrders method.
		//
		// Another advantage of this is we will be able to monitor the best prices,
		// spread, and mid price on Vega based on a set of orders that excludes
		// our orders, this way we can avoid feedback loops cause by our own
		// orders impacting the pricing of our subsequent orders.

		_ = liveExternalOrders

		var (
			highestBid int = 0
			lowestAsk  int = math.MaxInt
		)

		for _, order := range liveExternalOrders {
			price, err := strconv.Atoi(order.Price)
			if err != nil {
				log.Printf("Error parsing order price: %s", err)
				continue
			}
			if order.Side == vegapb.Side_SIDE_BUY && price > highestBid {
				highestBid = price
			}

			if order.Side == vegapb.Side_SIDE_SELL && price < lowestAsk {
				lowestAsk = price
			}
		}

		log.Printf("Highest External Bid: %d, Lowest External Ask: %d", highestBid, lowestAsk)

		vegaBestExternalBidAdj := decimal.NewFromInt(int64(highestBid)).Div(strat.d.priceFactor)
		vegaBestExternalAskAdj := decimal.NewFromInt(int64(lowestAsk)).Div(strat.d.priceFactor)
		vegaExternalMidAdj := vegaBestExternalBidAdj.Add(vegaBestExternalAskAdj).Div(decimal.NewFromInt(2))
		_ = vegaExternalMidAdj

		// Gradually reduce exposure over time.
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
					price = vegaBestExternalBidAdj
					priceMultiplier = decimal.NewFromFloat(0.998)
					bidOffset = bidOffset.Add(decimal.NewFromFloat(0.002))

					// We need to override the offset to be the difference between the
					// mid and the best external vega price, plus the difference between
					// 1 and our price multiplier.
					// bidOffset = vegaExternalMidAdj.Sub(vegaBestExternalBidAdj).Div(vegaExternalMidAdj).Abs().Add(decimal.NewFromFloat(0.0025))

				} else {
					side = vegapb.Side_SIDE_BUY
					price = vegaBestExternalAskAdj
					priceMultiplier = decimal.NewFromFloat(1.002)
					askOffset = askOffset.Add(decimal.NewFromFloat(0.002))

					// askOffset = vegaExternalMidAdj.Sub(vegaBestExternalAskAdj).Div(vegaExternalMidAdj).Abs().Add(decimal.NewFromFloat(0.0025))

				}

				if signedExposure.Abs().LessThan(decimal.NewFromInt(0)) {
					positionFraction = decimal.NewFromInt(1)
				}

				submissions = append(submissions, &commandspb.OrderSubmission{
					MarketId:    strat.VegaMarketId,
					Price:       price.Mul(priceMultiplier).Mul(strat.d.priceFactor).BigInt().String(),
					Size:        openVol.Abs().Mul(positionFraction).Mul(strat.d.positionFactor).BigInt().Uint64(),
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

		cancellations = append(cancellations, &commandspb.OrderCancellation{MarketId: strat.VegaMarketId})

		submissions = append(submissions,
			append(
				strat.GetOrderSubmission(vegapb.Side_SIDE_BUY, vegaBestExternalBidAdj, bidOffset, strat.TargetObligationVolume.Mul(strat.TargetVolCoefficient)),
				strat.GetOrderSubmission(vegapb.Side_SIDE_SELL, vegaBestExternalAskAdj, askOffset, strat.TargetObligationVolume.Mul(strat.TargetVolCoefficient))...,
			// strat.GetOrderSubmission(vegapb.Side_SIDE_BUY, vegaExternalMidAdj, bidOffset, strat.TargetObligationVolume.Mul(strat.TargetVolCoefficient)),
			// strat.GetOrderSubmission(vegapb.Side_SIDE_SELL, vegaExternalMidAdj, askOffset, strat.TargetObligationVolume.Mul(strat.TargetVolCoefficient))...,
			)...,
		)

		metricsData := &metrics.StrategyMetricsData{
			MarketId:              marketId,
			BinanceTicker:         strat.BinanceMarket,
			AgentPubkey:           strat.GetAgentPubKey(),
			Position:              strat.vegaStore.GetPosition(),
			SignedExposure:        signedExposure,
			VegaBestBid:           vegaBestBid.Div(strat.d.priceFactor),
			OurBestBid:            ourBestBidAdj,
			VegaBestAsk:           vegaBestAsk.Div(strat.d.priceFactor),
			OurBestAsk:            ourBestAskAdj,
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
