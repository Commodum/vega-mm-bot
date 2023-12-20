package strats

import (
	"log"
	"math"
	"strconv"
	"strings"
	"time"
	"vega-mm/data-engine"

	pd "code.vegaprotocol.io/quant/pricedistribution"
	"code.vegaprotocol.io/quant/riskmodelbs"
	vegapb "code.vegaprotocol.io/vega/protos/vega"
	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"

	"github.com/shopspring/decimal"
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

type StrategyOpts struct {
	marketId                string
	binanceMarket           string
	targetObligationVolume  int64
	maxProbabilityOfTrading float64
	orderSpacing            float64
	orderSizeBase           float64
	targetVolCoefficient    float64
	numOrdersPerSide        int
}

type strategy struct {
	vegaMarketId            string
	binanceMarket           string
	d                       *Decimals
	targetObligationVolume  decimal.Decimal
	maxProbabilityOfTrading decimal.Decimal
	orderSpacing            decimal.Decimal
	orderSizeBase           decimal.Decimal
	targetVolCoefficient    decimal.Decimal
	numOrdersPerSide        int

	vegaStore    *data.VegaStore
	binanceStore *data.BinanceStore

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
	GetVegaMarketId() string
	GetBinanceMarketTicker() string

	GetVegaStore() *data.VegaStore
	GetBinanceStore() *data.BinanceStore

	SetVegaDecimals(*Decimals)
	GetVegaDecimals(*vegapb.Market, *vegapb.Asset) *Decimals

	SubmitLiquidityCommitment()

	AmendLiquidityCommitment()

	GetOurBestBidAndAsk([]*vegapb.Order) (decimal.Decimal, decimal.Decimal)

	GetOrderSubmission(decimal.Decimal, decimal.Decimal, decimal.Decimal, decimal.Decimal, vegapb.Side, *vegapb.LogNormalRiskModel, *vegapb.LiquiditySLAParameters) []*commandspb.OrderSubmission

	// GetRiskMetricsForMarket(*vegapb.Market) (something... something... something...)

	GetEntryPriceAndVolume() (decimal.Decimal, decimal.Decimal)

	// GetProbabilityOfTradingForPrice() float64
}

type Decimals struct {
	positionFactor decimal.Decimal
	priceFactor    decimal.Decimal
	assetFactor    decimal.Decimal
}

// func NewStrategy(opts *StrategyOpts, config *Config) *strategy {
func NewStrategy(opts *StrategyOpts) *strategy {
	return &strategy{
		vegaMarketId:            opts.marketId,
		binanceMarket:           opts.binanceMarket,
		targetObligationVolume:  decimal.NewFromInt(opts.targetObligationVolume),
		maxProbabilityOfTrading: decimal.NewFromFloat(opts.maxProbabilityOfTrading),
		orderSpacing:            decimal.NewFromFloat(opts.orderSpacing),
		orderSizeBase:           decimal.NewFromFloat(opts.orderSizeBase),
		targetVolCoefficient:    decimal.NewFromFloat(opts.targetVolCoefficient),
		numOrdersPerSide:        opts.numOrdersPerSide,
		vegaStore:               data.NewVegaStore(opts.marketId),
		binanceStore:            data.NewBinanceStore(opts.binanceMarket),
	}
}

func (strat *strategy) GetVegaMarketId() string {
	return strat.vegaMarketId
}

func (strat *strategy) GetBinanceMarketTicker() string {
	return strat.binanceMarket
}

func (strat *strategy) GetVegaStore() *data.VegaStore {
	return strat.vegaStore
}

func (strat *strategy) GetBinanceStore() *data.BinanceStore {
	return strat.binanceStore
}

func (strat *strategy) SetVegaDecimals(d *Decimals) {
	strat.d = d
}

func (strat *strategy) GetVegaDecimals() *Decimals {
	return strat.d
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
			MarketId:         strat.vegaMarketId,
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
			MarketId: strat.vegaMarketId,
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
			MarketId:         strat.vegaMarketId,
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

func (strat *strategy) GetOrderSubmission(binanceRefPrice, vegaRefPrice, vegaMidPrice, offset, targetVolume, orderReductionAmount decimal.Decimal, side vegapb.Side, logNormalRiskModel *vegapb.LogNormalRiskModel, liquidityParams *vegapb.LiquiditySLAParameters) []*commandspb.OrderSubmission {

	vegaRefPriceAdj := vegaRefPrice.Div(strat.d.priceFactor)
	refPrice := vegaRefPriceAdj.InexactFloat64()

	log.Printf("Binance and Vega ref prices for %v: Binance: %v --- Vega: %v", side.String(), binanceRefPrice, vegaRefPrice.Div(strat.d.priceFactor))

	if side == vegapb.Side_SIDE_BUY && binanceRefPrice.LessThan(vegaRefPriceAdj.Mul(decimal.NewFromFloat(0.9995))) && !strat.binanceStore.isStale {
		// log.Printf("Using Binance ref price for bid...\n")
		// refPrice = binanceRefPrice.InexactFloat64()

		// Instead of changing the ref price we can just add to the offset.
		offset = offset.Add(decimal.NewFromInt(1).Sub(binanceRefPrice.Div(vegaRefPriceAdj)))

	} else if side == vegapb.Side_SIDE_SELL && binanceRefPrice.GreaterThan(vegaRefPriceAdj.Mul(decimal.NewFromFloat(1.0005))) && !strat.binanceStore.isStale {
		// log.Printf("Using Binance ref price for ask...\n")
		// refPrice = binanceRefPrice.InexactFloat64()

		offset = offset.Add(binanceRefPrice.Div(vegaRefPriceAdj).Sub(decimal.NewFromInt(1)))

	}

	priceTriggers := strat.vegaStore.GetMarket().GetPriceMonitoringSettings().GetParameters().GetTriggers()
	firstPrice := findPriceByProbabilityOfTrading(strat.maxProbabilityOfTrading, side, refPrice, logNormalRiskModel, priceTriggers)

	log.Printf("Calculated price: %v, Side: %v \n", firstPrice, side)
	log.Printf("Bid Threshold: %v\n", vegaMidPrice.Div(strat.d.priceFactor).Mul(decimal.NewFromFloat(1-(10./10000))))
	log.Printf("firstPrice: %v\n", firstPrice)

	// If the firstPrice is more then x basis points from the mid, set it to x-1 basis points from the mid.
	bp := 31.
	if side == vegapb.Side_SIDE_BUY {
		if decimal.NewFromFloat(firstPrice).LessThan(vegaMidPrice.Div(strat.d.priceFactor).Mul(decimal.NewFromFloat(1 - (bp / 10000)))) {
			firstPrice = vegaMidPrice.Div(strat.d.priceFactor).Mul(decimal.NewFromFloat(1 - ((bp - 1) / 10000))).InexactFloat64()
		}
	} else {
		if decimal.NewFromFloat(firstPrice).GreaterThan(vegaMidPrice.Div(strat.d.priceFactor).Mul(decimal.NewFromFloat(1 + (bp / 10000)))) {
			firstPrice = vegaMidPrice.Div(strat.d.priceFactor).Mul(decimal.NewFromFloat(1 + ((bp - 1) / 10000))).InexactFloat64()
		}
	}

	log.Printf("firstPrice: %v\n", firstPrice)
	// If offset would push the first order onto the other side of the book then set it's value such
	// that it's magnitude is the same size as the difference between the firstPrice and the refPrice.
	// This way the farthest that the offset can move the first order is to the front of the book and
	// not beyond
	if side == vegapb.Side_SIDE_BUY {

	} else {

	}

	riskParams := logNormalRiskModel.GetParams()

	reductionAmount := orderReductionAmount.Div(vegaRefPriceAdj)
	// reductionAmount = decimal.NewFromInt(0)
	reductionAmount = orderReductionAmount.Div(vegaRefPriceAdj).Mul(decimal.NewFromFloat(0.5))

	totalOrderSizeUnits := (math.Pow(strat.orderSizeBase.InexactFloat64(), float64(strat.numOrdersPerSide+1)) - float64(1)) / (strat.orderSizeBase.InexactFloat64() - float64(1))
	// totalOrderSizeUnits := (math.Pow(float64(2), float64(numOrders+1)) - float64(1)) / float64(2-1)
	orders := []*commandspb.OrderSubmission{}

	sizeF := func(i int) decimal.Decimal {

		size := targetVolume.Div(decimal.NewFromFloat(totalOrderSizeUnits).Mul(vegaRefPriceAdj)).Mul(strat.orderSizeBase.Pow(decimal.NewFromInt(int64(i + 1))))
		adjustedSize := decimal.NewFromInt(0)

		if size.LessThan(reductionAmount) {
			reductionAmount = decimal.Max(reductionAmount.Sub(size), decimal.NewFromInt(0))
			log.Printf("Reducing size of order by: %v\n", reductionAmount)
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

	log.Printf("%v targetVol: %v, refPrice: %v", strat.binanceMarket, targetVolume, refPrice)

	priceF := func(i int) decimal.Decimal {

		// TODO: Add a clamp so that the min/max price we quote will always be within the valid LP price range.

		return decimal.NewFromFloat(firstPrice).Mul(decimal.NewFromInt(1).Sub(decimal.NewFromInt(int64(i)).Mul(strat.orderSpacing)).Sub(offset))

	}

	if side == vegapb.Side_SIDE_SELL {

		priceF = func(i int) decimal.Decimal {

			// TODO: Add a clamp so that the min/max price we quote will always be within the valid LP price range.

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
	// distS := modelParams.GetProbabilityDistribution(refPrice, tauScaled)
	// Use the adj vega ref price for these probability calculations so we have accurate liquidity scores.
	distS := modelParams.GetProbabilityDistribution(vegaRefPriceAdj.InexactFloat64(), tauScaled)

	// bestAsk := decimal.RequireFromString(strat.vegaStore.GetMarketData().GetBestOfferPrice()).Div(strat.d.priceFactor)
	// maxAsk := bestAsk.InexactFloat64() * 6.0

	sumSize := 0.0
	sumSizeMulProb := 0.0

	for i := 0; i < strat.numOrdersPerSide; i++ {

		price := priceF(i)
		size := sizeF(i)

		orders = append(orders, &commandspb.OrderSubmission{
			MarketId:    strat.vegaMarketId,
			Price:       price.Mul(strat.d.priceFactor).BigInt().String(),
			Size:        size.Mul(strat.d.positionFactor).BigInt().Uint64(),
			Side:        side,
			TimeInForce: vegapb.Order_TIME_IN_FORCE_GTT,
			ExpiresAt:   int64(time.Now().UnixNano() + 10*1e9),
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
	log.Printf("%v Side: %v, volume weighted Liquidity Score: %v", strat.binanceMarket, side, volumeWeightedLiqScore)

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
			price = price - (refPrice * 0.0002)
			// price = price - (refPrice * 0.001)
		} else {
			price = price + (refPrice * 0.00025)
			// price = price + (refPrice * 0.001)
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

		// log.Printf("Side: %v, Price: %v, Last calculated probability: %v, maxProbability: %v", side, price, calculatedProb, probability)
	}

	log.Printf("Side: %v, Price: %v, Last calculated probability: %v, maxProbability: %v", side, price, calculatedProb, probability)

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
