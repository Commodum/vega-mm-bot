package strats

import (
	"log"
	"math"
	"strconv"
	"sync"
	"time"
	"vega-mm/metrics"
	"vega-mm/stores"

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

type OldStrategyOpts struct {
	marketId                string
	binanceMarket           string
	UsesLiquidityCommitment bool
	targetObligationVolume  int64
	maxProbabilityOfTrading float64
	orderSpacing            float64
	orderSizeBase           float64
	targetVolCoefficient    float64
	numOrdersPerSide        int
}

type GeneralOpts struct {
	AgentKeyPairIdx        uint64
	VegaMarketId           string
	BinanceMarket          string
	NumOrdersPerSide       int
	LiquidityCommitment    bool
	TargetObligationVolume decimal.Decimal
	TargetVolCoefficient   decimal.Decimal
}

type OptsType interface {
	~*MartingaleOpts | ~*OtherOpts
}

type OtherOpts struct {
	testOpt string
}

type Martingale *MartingaleOpts

type MartingaleOpts struct {
	MaxProbabilityOfTrading decimal.Decimal
	OrderSpacing            decimal.Decimal
	OrderSizeBase           decimal.Decimal
}

type StrategyOpts[T OptsType] struct {
	General  *GeneralOpts
	Specific T
}

type syncPubKeyBalance struct {
	mu    sync.RWMutex
	value decimal.Decimal
}

type MartingaleStrategy struct {
	d                  *decimals
	agentPubKey        string
	agentPubKeyBalance syncPubKeyBalance

	*GeneralOpts
	*MartingaleOpts

	vegaStore    *stores.VegaStore
	binanceStore *stores.BinanceStore

	txDataCh chan *commandspb.InputData
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
	SetTxDataChan(chan *commandspb.InputData)

	GetAgentKeyPairIndex() uint64

	GetVegaMarketId() string
	GetBinanceMarketTicker() string

	GetVegaStore() *stores.VegaStore
	GetBinanceStore() *stores.BinanceStore

	SetVegaDecimals(positionDecimals, priceDecimals, assetDecimals int64)
	GetVegaDecimals() *decimals

	UsesLiquidityCommitment() bool
	GetTargetObligationVolume() decimal.Decimal
	SubmitLiquidityCommitment()
	AmendLiquidityCommitment()
	CancelLiquidityCommitment()

	RunStrategy(chan *metrics.MetricsState)

	SetAgentPubKey(string)
	GetAgentPubKey() string
	SetAgentPubKeyBalance(decimal.Decimal)
	GetAgentPubKeyBalance() decimal.Decimal

	GetOurBestBidAndAsk([]*vegapb.Order) (decimal.Decimal, decimal.Decimal)

	GetOrderSubmission(decimal.Decimal, decimal.Decimal, decimal.Decimal, decimal.Decimal, decimal.Decimal, decimal.Decimal, vegapb.Side, *vegapb.LogNormalRiskModel, *vegapb.LiquiditySLAParameters) []*commandspb.OrderSubmission

	// GetRiskMetricsForMarket(*vegapb.Market) (something... something... something...)

	GetEntryPriceAndVolume() (decimal.Decimal, decimal.Decimal)

	// GetProbabilityOfTradingForPrice() float64
}

type decimals struct {
	positionFactor decimal.Decimal
	priceFactor    decimal.Decimal
	assetFactor    decimal.Decimal
}

// func NewStrategy(opts *StrategyOpts) *strategy {
func NewMartingaleStrategy(opts *StrategyOpts[Martingale]) *MartingaleStrategy {
	return &MartingaleStrategy{
		GeneralOpts:    opts.General,
		MartingaleOpts: opts.Specific,
		vegaStore:      stores.NewVegaStore(opts.General.VegaMarketId),
		binanceStore:   stores.NewBinanceStore(opts.General.BinanceMarket),
	}
}

func (strat *MartingaleStrategy) SetTxDataChan(ch chan *commandspb.InputData) {
	strat.txDataCh = ch
}

func (strat *MartingaleStrategy) GetAgentKeyPairIndex() uint64 {
	return strat.AgentKeyPairIdx
}

func (strat *MartingaleStrategy) GetVegaMarketId() string {
	return strat.VegaMarketId
}

func (strat *MartingaleStrategy) GetBinanceMarketTicker() string {
	return strat.BinanceMarket
}

func (strat *MartingaleStrategy) GetVegaStore() *stores.VegaStore {
	return strat.vegaStore
}

func (strat *MartingaleStrategy) GetBinanceStore() *stores.BinanceStore {
	return strat.binanceStore
}

func (strat *MartingaleStrategy) SetVegaDecimals(positionDecimals, priceDecimals, assetDecimals int64) {
	strat.d = &decimals{
		positionFactor: decimal.NewFromInt(10).Pow(decimal.NewFromInt(positionDecimals)),
		priceFactor:    decimal.NewFromInt(10).Pow(decimal.NewFromInt(priceDecimals)),
		assetFactor:    decimal.NewFromInt(10).Pow(decimal.NewFromInt(assetDecimals)),
	}
}

func (strat *MartingaleStrategy) GetVegaDecimals() *decimals {
	return strat.d
}

func (strat *MartingaleStrategy) UsesLiquidityCommitment() bool {
	return strat.LiquidityCommitment
}

func (strat *MartingaleStrategy) GetTargetObligationVolume() decimal.Decimal {
	return strat.TargetObligationVolume
}

func (strat *MartingaleStrategy) SubmitLiquidityCommitment() {

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

		// tx := strat.agent.signer.BuildTx(strat.agent.pubkey, inputData)

		// res := strat.agent.signer.SubmitTx(tx)

		// log.Printf("Response: %v", res)

	}
}

func (strat *MartingaleStrategy) AmendLiquidityCommitment() {
	if market := strat.vegaStore.GetMarket(); market != nil {

		stakeToCcyVolume, err := decimal.NewFromString(strat.vegaStore.GetStakeToCcyVolume())
		if err != nil {
			log.Fatalf("Failed to parse stakeToCcyVolume from string: %v", err)
		}

		// Create LP Amendment
		lpAmendment := &commandspb.LiquidityProvisionAmendment{
			MarketId:         strat.VegaMarketId,
			CommitmentAmount: strat.TargetObligationVolume.Mul(strat.d.assetFactor).Div(stakeToCcyVolume).BigInt().String(), // Divinde by stakeToCcyVolume
			Fee:              "0.0001",
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

		// tx := strat.agent.signer.BuildTx(strat.agent.pubkey, inputData)

		// res := strat.agent.signer.SubmitTx(tx)

		// log.Printf("Response: %v", res)

	}
}

func (strat *MartingaleStrategy) CancelLiquidityCommitment() {

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

		// tx := strat.agent.signer.BuildTx(strat.agent.pubkey, inputData)

		// res := strat.agent.signer.SubmitTx(tx)

		// log.Printf("Response: %v", res)

	}

}

// Locks not used because this should be writen once only.
func (s *MartingaleStrategy) SetAgentPubKey(pubkey string) {
	s.agentPubKey = pubkey
}

// No lock because this value should be written once only.
func (s *MartingaleStrategy) GetAgentPubKey() string {
	return s.agentPubKey
}

func (s *MartingaleStrategy) SetAgentPubKeyBalance(balance decimal.Decimal) {
	s.agentPubKeyBalance.mu.Lock()
	defer s.agentPubKeyBalance.mu.Unlock()
	s.agentPubKeyBalance.value = balance
}

func (s *MartingaleStrategy) GetAgentPubKeyBalance() decimal.Decimal {
	s.agentPubKeyBalance.mu.RLock()
	defer s.agentPubKeyBalance.mu.RUnlock()
	return s.agentPubKeyBalance.value
}

func (strat *MartingaleStrategy) GetOurBestBidAndAsk(liveOrders []*vegapb.Order) (decimal.Decimal, decimal.Decimal) {
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

func (strat *MartingaleStrategy) GetEntryPriceAndVolume() (volume, entryPrice decimal.Decimal) {

	if strat.vegaStore.GetPosition() == nil {
		return
	}

	volume = decimal.NewFromInt(strat.vegaStore.GetPosition().GetOpenVolume())
	entryPrice, _ = decimal.NewFromString(strat.vegaStore.GetPosition().GetAverageEntryPrice())

	return volume.Div(strat.d.positionFactor), entryPrice.Div(strat.d.priceFactor)
}

func (strat *MartingaleStrategy) GetPubkeyBalance(settlementAsset string) (b decimal.Decimal) {

	// marketId := maps.Keys(vega)[0]

	for _, acc := range strat.vegaStore.GetAccounts() {
		if acc.Owner != strat.agentPubKey || acc.Asset != settlementAsset {
			continue
		}

		balance, _ := decimal.NewFromString(acc.Balance)
		b = b.Add(balance)
	}

	return b.Div(strat.d.assetFactor)
}

func (strat *MartingaleStrategy) GetOrderSubmission(binanceRefPrice, vegaRefPrice, vegaMidPrice, offset, targetVolume, orderReductionAmount decimal.Decimal, side vegapb.Side, logNormalRiskModel *vegapb.LogNormalRiskModel, liquidityParams *vegapb.LiquiditySLAParameters) []*commandspb.OrderSubmission {

	vegaRefPriceAdj := vegaRefPrice.Div(strat.d.priceFactor)
	refPrice := vegaRefPriceAdj.InexactFloat64()

	log.Printf("Binance and Vega ref prices for %v: Binance: %v --- Vega: %v", side.String(), binanceRefPrice, vegaRefPrice.Div(strat.d.priceFactor))

	if side == vegapb.Side_SIDE_BUY && binanceRefPrice.LessThan(vegaRefPriceAdj.Mul(decimal.NewFromFloat(0.9995))) && !strat.binanceStore.IsStale() {
		// log.Printf("Using Binance ref price for bid...\n")
		// refPrice = binanceRefPrice.InexactFloat64()

		// Instead of changing the ref price we can just add to the offset.
		offset = offset.Add(decimal.NewFromInt(1).Sub(binanceRefPrice.Div(vegaRefPriceAdj)))

	} else if side == vegapb.Side_SIDE_SELL && binanceRefPrice.GreaterThan(vegaRefPriceAdj.Mul(decimal.NewFromFloat(1.0005))) && !strat.binanceStore.IsStale() {
		// log.Printf("Using Binance ref price for ask...\n")
		// refPrice = binanceRefPrice.InexactFloat64()

		offset = offset.Add(binanceRefPrice.Div(vegaRefPriceAdj).Sub(decimal.NewFromInt(1)))

	}

	priceTriggers := strat.vegaStore.GetMarket().GetPriceMonitoringSettings().GetParameters().GetTriggers()
	firstPrice := findPriceByProbabilityOfTrading(strat.MaxProbabilityOfTrading, side, refPrice, logNormalRiskModel, priceTriggers)

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

	totalOrderSizeUnits := (math.Pow(strat.OrderSizeBase.InexactFloat64(), float64(strat.NumOrdersPerSide+1)) - float64(1)) / (strat.OrderSizeBase.InexactFloat64() - float64(1))
	// totalOrderSizeUnits := (math.Pow(float64(2), float64(numOrders+1)) - float64(1)) / float64(2-1)
	orders := []*commandspb.OrderSubmission{}

	sizeF := func(i int) decimal.Decimal {

		size := targetVolume.Div(decimal.NewFromFloat(totalOrderSizeUnits).Mul(vegaRefPriceAdj)).Mul(strat.OrderSizeBase.Pow(decimal.NewFromInt(int64(i + 1))))
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

	log.Printf("%v targetVol: %v, refPrice: %v", strat.BinanceMarket, targetVolume, refPrice)

	priceF := func(i int) decimal.Decimal {

		// TODO: Add a clamp so that the min/max price we quote will always be within the valid LP price range.

		return decimal.NewFromFloat(firstPrice).Mul(decimal.NewFromInt(1).Sub(decimal.NewFromInt(int64(i)).Mul(strat.OrderSpacing)).Sub(offset))

	}

	if side == vegapb.Side_SIDE_SELL {

		priceF = func(i int) decimal.Decimal {

			// TODO: Add a clamp so that the min/max price we quote will always be within the valid LP price range.

			return decimal.NewFromFloat(firstPrice).Mul(decimal.NewFromInt(1).Add(decimal.NewFromInt(int64(i)).Mul(strat.OrderSpacing)).Add(offset))
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
	log.Printf("%v Side: %v, volume weighted Liquidity Score: %v", strat.BinanceMarket, side, volumeWeightedLiqScore)

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

func (strat *MartingaleStrategy) RunStrategy(metricsCh chan *metrics.MetricsState) {

	for range time.NewTicker(750 * time.Millisecond).C {
		log.Printf("Executing strategy for %v...", strat.BinanceMarket)

		var (
			marketId        = strat.VegaMarketId
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
			bidVol = strat.TargetObligationVolume.Mul(strat.TargetVolCoefficient)
			askVol = strat.TargetObligationVolume.Mul(strat.TargetVolCoefficient)
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

		strat.SetAgentPubKeyBalance(balance)

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
			value := strat.GetAgentPubKeyBalance().Mul(decimal.NewFromFloat(threshold))
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
				MarketId: strat.VegaMarketId,
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

		cancellations = append(cancellations, &commandspb.OrderCancellation{MarketId: strat.VegaMarketId})

		submissions = append(
			submissions,
			append(
				strat.GetOrderSubmission(binanceBestBid, vegaBestBid, vegaMidPrice, bidOffset, bidVol, bidReductionAmount, vegapb.Side_SIDE_BUY, logNormalRiskModel, liquidityParams),
				strat.GetOrderSubmission(binanceBestAsk, vegaBestAsk, vegaMidPrice, askOffset, askVol, askReductionAmount, vegapb.Side_SIDE_SELL, logNormalRiskModel, liquidityParams)...,
			)...,
		)

		state := &metrics.MetricsState{
			MarketId:              marketId,
			BinanceTicker:         strat.BinanceMarket,
			Position:              strat.vegaStore.GetPosition(),
			SignedExposure:        signedExposure,
			VegaBestBid:           vegaBestBid.Div(strat.d.priceFactor),
			OurBestBid:            ourBestBid,
			VegaBestAsk:           vegaBestAsk.Div(strat.d.priceFactor),
			OurBestAsk:            ourBestAsk,
			LiveOrdersCount:       len(strat.vegaStore.GetOrders()),
			MarketDataUpdateCount: int(strat.vegaStore.GetMarketDataUpdateCounter()),
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

		strat.txDataCh <- inputData

		// tx := a.signer.BuildTx(a.pubkey, inputData)

		// res := a.signer.SubmitTx(tx)

		// log.Printf("%v Tx Response: %v", strat.BinanceMarket, res)

	}

	return
}
