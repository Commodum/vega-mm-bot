package main

import (
	"context"
	// "sync"
	// "encoding/json"
	"log"
	"math"

	// "reflect"
	"strconv"
	"strings"
	"time"

	// "code.vegaprotocol.io/quant/interfaces"
	pd "code.vegaprotocol.io/quant/pricedistribution"
	"code.vegaprotocol.io/quant/riskmodelbs"
	vegapb "code.vegaprotocol.io/vega/protos/vega"
	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"
	walletpb "code.vegaprotocol.io/vega/protos/vega/wallet/v1"
	"github.com/jeremyletang/vega-go-sdk/wallet"
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
//		 Params:
//			- orderSizeBase
//			- maxProbabilityOfTrading
//			- orderSpacing
//			- targetVolume (bidvol, askVol)
//

type agent struct {
	// An agent will control one wallet and can run multiple strategies
	// Note: For the time being an agent will only allow one strategy per market.
	pubkey       string
	balance      decimal.Decimal
	strategies   map[string]*strategy
	vegaClient   *VegaClient
	walletClient *wallet.Client
	config       *Config
	metricsCh    chan *MetricsState
}

type Agent interface {
	// Registers a strategy with the agent
	RegisterStrategy(*strategy)

	// Checks current state of Liquidity Commitment, submits one if it does not exist,
	// amends it if there are any changes, or does nothing if there are no changes.
	UpdateLiquidityCommitment(*wallet.Client, *strategy)

	// Runs the provided strategy
	RunStrategy(*strategy, chan *MetricsState)
}

type StrategyOpts struct {
	lpCommitmentSizeUSD     int64
	maxProbabilityOfTrading float64
	orderSpacing            float64
	orderSizeBase           float64
	targetVolCoefficient    float64
	numOrdersPerSide        int
}

type strategy struct {
	marketId                string
	d                       *decimals
	lpCommitmentSizeUSD     decimal.Decimal
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

	SubmitLiquidityCommitment(*wallet.Client)

	AmendLiquidityCommitment(*wallet.Client)

	// GetOrderSubmissionForSide() []*commandspb.OrderSubmission

	// GetRiskMetricsForMarket(*vegapb.Market) (something... something... something...)

	// GetEntryPriceAndVolume() (decimal.Decimal, decimal.Decimal)

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
	return
}

func (a *agent) UpdateLiquidityCommitment(walletClient *wallet.Client, strat *strategy) {
	lpCommitment := strat.vegaStore.GetLiquidityProvision()

	switch true {
	case (lpCommitment != nil && strat.lpCommitmentSizeUSD.IsZero()):
		// strat.CancelLiquidityCommitment(walletClient)
	case (lpCommitment == nil && !strat.lpCommitmentSizeUSD.IsZero()):
		strat.SubmitLiquidityCommitment(walletClient)
	case (lpCommitment != nil && !strat.lpCommitmentSizeUSD.IsZero()):
		strat.AmendLiquidityCommitment(walletClient)
	default:
		return
	}
}

func (a *agent) RunStrategy(strat *strategy, metricsCh chan *MetricsState) {

	for range time.NewTicker(1500 * time.Millisecond).C {
		log.Printf("Executing strategy...")

		var (
			marketId        = strat.marketId
			market          = strat.vegaStore.GetMarket()
			settlementAsset = market.GetTradableInstrument().GetInstrument().GetPerpetual().GetSettlementAsset()

			logNormalRiskModel     = market.GetTradableInstrument().GetLogNormalRiskModel()
			liveOrders             = strat.vegaStore.GetOrders()
			vegaBestBid            = decimal.RequireFromString(strat.vegaStore.GetMarketData().GetBestBidPrice())
			vegaBestAsk            = decimal.RequireFromString(strat.vegaStore.GetMarketData().GetBestOfferPrice())
			vegaSpread             = vegaBestAsk.Sub(vegaBestBid)
			ourBestBid, ourBestAsk = strat.getOurBestBidAndAsk(liveOrders)
			// ourSpread              = ourBestAsk.Sub(ourBestBid)
			openVol, avgEntryPrice = strat.getEntryPriceAndVolume()
			notionalExposure       = avgEntryPrice.Mul(openVol).Abs()
			signedExposure         = avgEntryPrice.Mul(openVol)
			balance                = getPubkeyBalance(strat.vegaStore, strat.agent.pubkey, settlementAsset, strat.d.assetFactor)
			bidVol                 = balance.Mul(strat.targetVolCoefficient)
			askVol                 = balance.Mul(strat.targetVolCoefficient)
			neutralityThresholds   = []float64{0.02, 0.05, 0.1}
			neutralityOffsets      = []float64{0.002, 0.004, 0.008}
			bidReductionAmount     = decimal.Max(signedExposure, decimal.NewFromInt(0))
			askReductionAmount     = decimal.Min(signedExposure, decimal.NewFromInt(0)).Abs()
			bidOffset              = decimal.NewFromInt(0)
			askOffset              = decimal.NewFromInt(0)

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
			case signedExposure.LessThan(value.Neg()):
				// Too much short exposure, step back ask
				askOffset = decimal.NewFromFloat(neutralityOffsets[i])
			}
		}

		log.Printf("bidOffset: %v, askOffset: %v", bidOffset, askOffset)

		// We want to stay neutral so we place an order at the best bid or best ask with size equal to our
		// exposure, this will close out our exposure as soon as possible.
		// Note: We could later on make this a set of orders with a martingale distribution over a tight price
		//		 very close to the best bid. This could make it slower to close out but may give us better
		//		 execution. Or we could just push our existing orders on that side to the front of the book
		//		 by modifying the offset for that side.
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
				ExpiresAt:   int64(time.Now().UnixNano() + 5*1e9),
				Type:        vegapb.Order_TYPE_LIMIT,
				PostOnly:    true,
				Reference:   "ref",
			})

		}

		cancellations = append(cancellations, &commandspb.OrderCancellation{MarketId: strat.marketId})

		submissions = append(
			submissions,
			append(
				strat.getOrderSubmission(vegaBestBid, bidOffset, bidVol, bidReductionAmount, vegapb.Side_SIDE_BUY, logNormalRiskModel),
				strat.getOrderSubmission(vegaBestAsk, askOffset, askVol, askReductionAmount, vegapb.Side_SIDE_SELL, logNormalRiskModel)...,
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

		// Send transaction
		err := strat.agent.walletClient.SendTransaction(
			context.Background(), strat.agent.pubkey, &walletpb.SubmitTransactionRequest{
				Command: &walletpb.SubmitTransactionRequest_BatchMarketInstructions{
					BatchMarketInstructions: &batch,
				},
			},
		)

		if err != nil {
			log.Printf("Error subitting the batch: %v", err)
		}

	}

	return
}

func (a *agent) StartMetrics() chan *MetricsState {
	return make(chan *MetricsState)
}

func NewAgent(pubkey string, walletClient *wallet.Client, config *Config) Agent {
	agent := &agent{
		pubkey:     pubkey,
		strategies: map[string]*strategy{},
		vegaClient: &VegaClient{
			grpcAddr:      config.VegaGrpcAddr,
			grpcAddresses: strings.Split(config.VegaGrpcAddresses, ","),
			vegaMarkets:   []string{},
			reconnChan:    make(chan struct{}),
			reconnecting:  false,
		},
		walletClient: walletClient,
		config:       config,
		metricsCh:    make(chan *MetricsState),
	}
	agent.vegaClient.agent = agent
	return agent
}

func (s *strategy) GetDecimals(market *vegapb.Market, asset *vegapb.Asset) {
	s.d = &decimals{
		positionFactor: decimal.NewFromInt(10).Pow(decimal.NewFromInt(market.PositionDecimalPlaces)),
		priceFactor:    decimal.NewFromInt(10).Pow(decimal.NewFromInt(int64(market.DecimalPlaces))),
		assetFactor:    decimal.NewFromInt(10).Pow(decimal.NewFromInt(int64(asset.Details.Decimals))),
	}
}

func NewStrategy(marketId string, opts *StrategyOpts, config *Config) *strategy {
	return &strategy{
		marketId:                marketId,
		lpCommitmentSizeUSD:     decimal.NewFromInt(opts.lpCommitmentSizeUSD),
		maxProbabilityOfTrading: decimal.NewFromFloat(opts.maxProbabilityOfTrading),
		orderSpacing:            decimal.NewFromFloat(opts.orderSpacing),
		orderSizeBase:           decimal.NewFromFloat(opts.orderSizeBase),
		targetVolCoefficient:    decimal.NewFromFloat(opts.targetVolCoefficient),
		numOrdersPerSide:        opts.numOrdersPerSide,
		vegaStore:               newVegaStore(marketId),
	}
}

func (strat *strategy) AmendLiquidityCommitment(walletClient *wallet.Client) {
	if market := strat.vegaStore.GetMarket(); market != nil {

		// Create LP submission
		lpAmendment := &commandspb.LiquidityProvisionAmendment{
			MarketId:         strat.marketId,
			CommitmentAmount: strat.lpCommitmentSizeUSD.Mul(strat.d.assetFactor).BigInt().String(),
			Fee:              "0.0001",
			Sells:            []*vegapb.LiquidityOrder{}, // getLiquidityOrders(vegapb.Side_SIDE_SELL, strat.vegaStore),
			Buys:             []*vegapb.LiquidityOrder{}, // getLiquidityOrders(vegapb.Side_SIDE_BUY, strat.vegaStore),
			Reference:        "Opportunities don't happen, you create them.",
		}

		// Submit transaction
		err := walletClient.SendTransaction(
			context.Background(), strat.agent.pubkey, &walletpb.SubmitTransactionRequest{
				Command: &walletpb.SubmitTransactionRequest_LiquidityProvisionAmendment{
					LiquidityProvisionAmendment: lpAmendment,
				},
			},
		)

		if err != nil {
			log.Fatalf("Failed to send transaction: failed to amend liquidity commitment: %v", err)
		}
	}
}

func (strat *strategy) SubmitLiquidityCommitment(walletClient *wallet.Client) {

	if market := strat.vegaStore.GetMarket(); market != nil {

		// Create LP submission
		lpSubmission := &commandspb.LiquidityProvisionSubmission{
			MarketId:         strat.marketId,
			CommitmentAmount: strat.lpCommitmentSizeUSD.Mul(strat.d.assetFactor).BigInt().String(),
			Fee:              "0.0001",
			Sells:            getLiquidityOrders(vegapb.Side_SIDE_SELL, strat.vegaStore), // []*vegapb.LiquidityOrder{},
			Buys:             getLiquidityOrders(vegapb.Side_SIDE_BUY, strat.vegaStore),  // []*vegapb.LiquidityOrder{},
			Reference:        "Opportunities don't happen, you create them.",
		}

		// Submit transaction
		err := walletClient.SendTransaction(
			context.Background(), strat.agent.pubkey, &walletpb.SubmitTransactionRequest{
				Command: &walletpb.SubmitTransactionRequest_LiquidityProvisionSubmission{
					LiquidityProvisionSubmission: lpSubmission,
				},
			},
		)

		if err != nil {
			log.Fatalf("Failed to send transaction: failed to submit liquidity commitment: %v", err)
		}
	}
}

func getLiquidityOrders(side vegapb.Side, vegaStore *VegaStore) []*vegapb.LiquidityOrder {

	offset := 0.0015
	numOrders := 5

	sizeF := func(i int) decimal.Decimal {
		return decimal.NewFromInt(2).Pow(decimal.NewFromInt(int64(i)))
	}

	vegaBestBid, _ := decimal.NewFromString(vegaStore.GetMarketData().GetBestBidPrice())

	offsetF := func(i int) decimal.Decimal {
		if i == 1 {
			return decimal.NewFromFloat(0.0015).Mul(vegaBestBid)
		}
		return decimal.NewFromFloat(offset).Mul(decimal.NewFromInt(int64(i))).Mul(vegaBestBid)
	}

	referenceF := func(side vegapb.Side) vegapb.PeggedReference {
		if side == vegapb.Side_SIDE_BUY {
			return vegapb.PeggedReference_PEGGED_REFERENCE_BEST_BID
		}
		return vegapb.PeggedReference_PEGGED_REFERENCE_BEST_ASK
	}

	orders := []*vegapb.LiquidityOrder{}

	for i := 1; i <= numOrders; i++ {
		orders = append(orders, &vegapb.LiquidityOrder{
			Reference:  referenceF(side),
			Proportion: uint32(sizeF(i).BigInt().Uint64()),
			Offset:     offsetF(i).BigInt().String(),
		})
	}

	return orders

}

func (strat *strategy) getOurBestBidAndAsk(liveOrders []*vegapb.Order) (decimal.Decimal, decimal.Decimal) {
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

func (strat *strategy) getEntryPriceAndVolume() (volume, entryPrice decimal.Decimal) {

	if strat.vegaStore.GetPosition() == nil {
		return
	}

	volume = decimal.NewFromInt(strat.vegaStore.GetPosition().GetOpenVolume())
	entryPrice, _ = decimal.NewFromString(strat.vegaStore.GetPosition().GetAverageEntryPrice())

	return volume.Div(strat.d.positionFactor), entryPrice.Div(strat.d.priceFactor)
}

func getPubkeyBalance(vegaStore *VegaStore, pubkey, settlementAsset string, assetFactor decimal.Decimal) (b decimal.Decimal) {

	// marketId := maps.Keys(vega)[0]

	for _, acc := range vegaStore.GetAccounts() {
		if acc.Owner != pubkey || acc.Asset != settlementAsset {
			continue
		}

		balance, _ := decimal.NewFromString(acc.Balance)
		b = b.Add(balance)
	}

	return b.Div(assetFactor)
}

func (strat *strategy) getOrderSubmission(vegaRefPrice, offset, targetVolume, orderReductionAmount decimal.Decimal, side vegapb.Side, logNormalRiskModel *vegapb.LogNormalRiskModel) []*commandspb.OrderSubmission {

	refPrice, _ := vegaRefPrice.Div(strat.d.priceFactor).Float64()
	firstPrice := findPriceByProbabilityOfTrading(strat.maxProbabilityOfTrading, side, refPrice, logNormalRiskModel)

	log.Printf("Calculated price: %v, Side: %v \n", firstPrice, side)

	reductionAmount := orderReductionAmount.Div(vegaRefPrice.Div(strat.d.priceFactor))

	totalOrderSizeUnits := (math.Pow(strat.orderSizeBase.InexactFloat64(), float64(strat.numOrdersPerSide+1)) - float64(1)) / (strat.orderSizeBase.InexactFloat64() - float64(1))
	// totalOrderSizeUnits := (math.Pow(float64(2), float64(numOrders+1)) - float64(1)) / float64(2-1)
	orders := []*commandspb.OrderSubmission{}

	sizeF := func(i int) decimal.Decimal {

		size := targetVolume.Div(decimal.NewFromFloat(totalOrderSizeUnits).Mul(vegaRefPrice.Div(strat.d.priceFactor))).Mul(strat.orderSizeBase.Pow(decimal.NewFromInt(int64(i))))
		adjustedSize := decimal.NewFromInt(0)

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

	log.Printf("targetVol; %v, vegaRefPrice: %v", targetVolume, vegaRefPrice)

	priceF := func(i int) decimal.Decimal {

		if i == 1 && offset.IsZero() {
			return decimal.NewFromFloat(firstPrice)
		}
		return decimal.NewFromFloat(firstPrice).Mul(decimal.NewFromInt(1).Sub(decimal.NewFromInt(int64(i)).Mul(strat.orderSpacing)).Sub(offset))

	}

	if side == vegapb.Side_SIDE_SELL {

		priceF = func(i int) decimal.Decimal {
			if i == 1 && offset.IsZero() {
				return decimal.NewFromFloat(firstPrice)
			}
			return decimal.NewFromFloat(firstPrice).Mul(decimal.NewFromInt(1).Add(decimal.NewFromInt(int64(i)).Mul(strat.orderSpacing)).Add(offset))
		}

	}

	for i := 1; i <= strat.numOrdersPerSide; i++ {
		orders = append(orders, &commandspb.OrderSubmission{
			MarketId:    strat.marketId,
			Price:       priceF(i).Mul(strat.d.priceFactor).BigInt().String(),
			Size:        sizeF(i).Mul(strat.d.positionFactor).BigInt().Uint64(),
			Side:        side,
			TimeInForce: vegapb.Order_TIME_IN_FORCE_GTT,
			ExpiresAt:   int64(time.Now().UnixNano() + 5*1e9),
			Type:        vegapb.Order_TYPE_LIMIT,
			PostOnly:    true,
			Reference:   "ref",
		})
	}

	return orders
}

// func getOrderSubmission(d decimals, ourBestPrice int, vegaSpread, vegaRefPrice, binanceRefPrice, offset, targetVolume, orderReductionAmount decimal.Decimal, side vegapb.Side, marketId string, riskParams *vegapb.LogNormalModelParams, tau float64) []*commandspb.OrderSubmission {

// 	firstOrderProbabilityOfTrading := decimal.NewFromFloat(0.8)

// 	refPrice, _ := vegaRefPrice.Div(d.priceFactor).Float64()

// 	firstPrice := findPriceByProbabilityOfTrading(firstOrderProbabilityOfTrading, side, refPrice, riskParams, tau)

// 	log.Printf("Calculated price: %v, Side: %v \n", firstPrice, side)

// 	reductionAmount := orderReductionAmount.Div(vegaRefPrice.Div(d.priceFactor))

// 	orderSpacing := decimal.NewFromFloat(0.001)

// 	orderSizeBase := 1.5

// 	numOrders := 6
// 	totalOrderSizeUnits := (math.Pow(orderSizeBase, float64(numOrders+1)) - float64(1)) / (orderSizeBase - float64(1))
// 	// totalOrderSizeUnits := (math.Pow(float64(2), float64(numOrders+1)) - float64(1)) / float64(2-1)
// 	orders := []*commandspb.OrderSubmission{}

// 	sizeF := func(i int) decimal.Decimal {

// 		size := targetVolume.Div(decimal.NewFromFloat(totalOrderSizeUnits).Mul(vegaRefPrice.Div(d.priceFactor))).Mul(decimal.NewFromFloat(orderSizeBase).Pow(decimal.NewFromInt(int64(i))))
// 		adjustedSize := decimal.NewFromInt(0)

// 		if size.LessThan(reductionAmount) {
// 			reductionAmount = decimal.Max(reductionAmount.Sub(size), decimal.NewFromInt(0))
// 		} else if size.GreaterThan(reductionAmount) {
// 			adjustedSize = size.Sub(reductionAmount)
// 			reductionAmount = decimal.NewFromInt(0)
// 		} else {
// 			reductionAmount = decimal.NewFromInt(0)
// 		}

// 		return decimal.Max(
// 			adjustedSize,
// 			decimal.NewFromInt(1).Div(d.positionFactor),
// 		)
// 	}

// 	// sizeF := func() decimal.Decimal {
// 	// 	return decimal.Max(targetVolume.Div(decimal.NewFromInt(int64(numOrders)).Mul(vegaRefPrice.Div(d.priceFactor))), decimal.NewFromInt(1).Div(d.positionFactor))
// 	// }

// 	log.Printf("targetVol; %v, vegaRefPrice: %v", targetVolume, vegaRefPrice)

// 	priceF := func(i int) decimal.Decimal {

// 		if i == 1 && offset.IsZero() {
// 			log.Printf("Our best bid: %v", ourBestPrice)
// 			return decimal.NewFromFloat(firstPrice)
// 		}
// 		return decimal.NewFromFloat(firstPrice).Mul(decimal.NewFromInt(1).Sub(decimal.NewFromInt(int64(i)).Mul(orderSpacing)).Sub(offset))

// 	}

// 	if side == vegapb.Side_SIDE_SELL {

// 		priceF = func(i int) decimal.Decimal {
// 			if i == 1 && offset.IsZero() {
// 				log.Printf("Our best ask: %v", ourBestPrice)
// 				return decimal.NewFromFloat(firstPrice)
// 			}
// 			return decimal.NewFromFloat(firstPrice).Mul(decimal.NewFromInt(1).Add(decimal.NewFromInt(int64(i)).Mul(orderSpacing)).Add(offset))
// 		}

// 	}

// 	for i := 1; i <= numOrders; i++ {
// 		orders = append(orders, &commandspb.OrderSubmission{
// 			MarketId:    marketId,
// 			Price:       priceF(i).Mul(d.priceFactor).BigInt().String(),
// 			Size:        sizeF(i).Mul(d.positionFactor).BigInt().Uint64(),
// 			Side:        side,
// 			TimeInForce: vegapb.Order_TIME_IN_FORCE_GTT,
// 			ExpiresAt:   int64(time.Now().UnixNano() + 5*1e9),
// 			Type:        vegapb.Order_TYPE_LIMIT,
// 			PostOnly:    true,
// 			Reference:   "ref",
// 		})
// 	}

// 	return orders
// }

func findPriceByProbabilityOfTrading(probability decimal.Decimal, side vegapb.Side, refPrice float64, logNormalRiskModel *vegapb.LogNormalRiskModel) (price float64) {

	tau := logNormalRiskModel.GetTau()
	riskParams := logNormalRiskModel.GetParams()
	log.Printf("desiredMaxProbability: %v, refPrice: %v, tau: %v", probability, refPrice, tau)

	modelParams := riskmodelbs.ModelParamsBS{
		Mu:    riskParams.Mu,
		R:     riskParams.R,
		Sigma: riskParams.Sigma,
	}
	// Get analytical distribution
	dist := modelParams.GetProbabilityDistribution(refPrice, tau)

	// Get price range from distribution
	minPrice, maxPrice := pd.PriceRange(dist, probability.InexactFloat64())
	log.Printf("minPrice: %v, maxPrice: %v", minPrice, maxPrice)

	price = refPrice
	calculatedProb := float64(1)
	// Iterate through prices until we find a price that corresponds to our desired probability.
	for calculatedProb > probability.InexactFloat64() {
		if side == vegapb.Side_SIDE_BUY {
			price = price - float64(price*0.0002)
		} else {
			price = price + float64(price*0.0002)
		}
		calculatedProb = getProbabilityOfTradingForOrder(riskParams.Mu, riskParams.Sigma, tau, minPrice, maxPrice, price, refPrice, side)
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
