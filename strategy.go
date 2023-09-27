package main

import (
	"context"
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
	// "golang.org/x/exp/maps"
)

type decimals struct {
	positionFactor decimal.Decimal
	priceFactor    decimal.Decimal
}

func RunStrategy(walletClient *wallet.Client, dataClient *DataClient) {

	for range time.NewTicker(1500 * time.Millisecond).C {
		log.Printf("Executing strategy...")

		var (
			marketIds      = strings.Split(dataClient.c.VegaMarkets, ",")
			binanceMarkets = strings.Split(dataClient.c.BinanceMarkets, ",")
			pubkey         = dataClient.c.WalletPubkey
			submissions    = []*commandspb.OrderSubmission{}
			cancellations  = []*commandspb.OrderCancellation{}
		)

		for i, marketId := range marketIds {

			if marketId != dataClient.c.LpMarket {
				continue
			}

			liveOrders := dataClient.s.v[marketId].GetOrders()
			ourBestBid := 0
			ourBestAsk := math.MaxInt

			log.Println("numLiveOrders: ", len(liveOrders))
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

			log.Printf("Our best bid: %v, Our best ask: %v", ourBestBid, ourBestAsk)

			// Get market
			if market := dataClient.s.v[marketId].GetMarket(); market != nil {
				settlementAsset := market.GetTradableInstrument().
					GetInstrument().
					GetFuture().
					GetSettlementAsset()
				asset := dataClient.s.v[marketId].GetAsset(settlementAsset)
				logNormalRiskModel := market.GetTradableInstrument().
					GetLogNormalRiskModel()
				tau := logNormalRiskModel.GetTau()
				riskParams := logNormalRiskModel.GetParams()

				d := getDecimals(market, asset)

				vegaBestBid, _ := decimal.NewFromString(dataClient.s.v[marketId].GetMarketData().GetBestBidPrice())
				vegaBestAsk, _ := decimal.NewFromString(dataClient.s.v[marketId].GetMarketData().GetBestOfferPrice())
				vegaSpread := vegaBestAsk.Sub(vegaBestBid)
				binanceBestBid, binanceBestAsk := dataClient.s.b[binanceMarkets[i]].Get()

				log.Printf("Vega best bid: %v, Vega best ask: %v", vegaBestBid, vegaBestAsk)
				log.Printf("Vega spread: %v", vegaSpread)

				openVol, avgEntryPrice := getEntryPriceAndVolume(d, market, dataClient.s.v[marketId].GetPosition())
				notionalExposure := avgEntryPrice.Mul(openVol).Abs()
				signedExposure := avgEntryPrice.Mul(openVol)
				balance := getPubkeyBalance(marketId, dataClient.s.v, pubkey, asset.Id, int64(asset.Details.Decimals))

				// Determine order sizing from position and balance.
				bidVol := balance.Mul(decimal.NewFromFloat(1.25))
				askVol := balance.Mul(decimal.NewFromFloat(1.25))

				log.Printf("Balance: %v", balance)
				log.Printf("Binance best bid: %v, Binance best ask: %v", binanceBestBid, binanceBestAsk)
				log.Printf("Open volume: %v, entry price: %v, notional exposure: %v", openVol, avgEntryPrice, notionalExposure)
				log.Printf("Bid volume: %v, ask volume: %v", bidVol, askVol)

				// Use the current position to determine the offset from the reference price for each order submission.
				// If we are exposed long then asks have no offset while bids have an offset. Vice versa for short exposure.
				// If exposure is below a threshold in either direction then there set both offsets to 0.
				neutralityThreshold := 0.02
				bidOffset := decimal.NewFromInt(0)
				askOffset := decimal.NewFromInt(0)

				switch true {
				case signedExposure.LessThan(balance.Mul(decimal.NewFromFloat(neutralityThreshold)).Mul(decimal.NewFromInt(-1))):
					// Step back ask
					askOffset = decimal.NewFromFloat(0.002)
					break
				case signedExposure.GreaterThan(balance.Mul(decimal.NewFromFloat(neutralityThreshold))):
					// Step back bid
					bidOffset = decimal.NewFromFloat(0.002)
					break
				}

				log.Printf("bidOffset: %v, askOffset: %v", bidOffset, askOffset)

				// We want to stay as neutral as possible even below the neutrality threshold, so we should place an
				// order at the best bid or best ask with size equal to our exposure, this will close out our
				// exposure as soon as possible.
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
						MarketId:    marketId,
						Price:       price.BigInt().String(),
						Size:        openVol.Div(price).Mul(d.priceFactor).Mul(d.positionFactor).BigInt().Uint64(),
						Side:        side,
						TimeInForce: vegapb.Order_TIME_IN_FORCE_GTT,
						ExpiresAt:   int64(time.Now().UnixNano() + 5*1e9),
						Type:        vegapb.Order_TYPE_LIMIT,
						PostOnly:    true,
						Reference:   "ref",
					})

				}

				cancellations = append(cancellations, &commandspb.OrderCancellation{MarketId: marketId})

				submissions = append(
					submissions,
					append(
						getOrderSubmission(d, ourBestBid, vegaSpread, vegaBestBid, binanceBestBid, bidOffset, bidVol, vegapb.Side_SIDE_BUY, marketId, riskParams, tau),
						getOrderSubmission(d, ourBestAsk, vegaSpread, vegaBestAsk, binanceBestAsk, askOffset, askVol, vegapb.Side_SIDE_SELL, marketId, riskParams, tau)...,
					)...,
				)

				log.Printf("%v", submissions)
			}
		}

		batch := commandspb.BatchMarketInstructions{
			Cancellations: cancellations,
			Submissions:   submissions,
		}

		// Send transaction
		err := walletClient.SendTransaction(
			context.Background(), pubkey, &walletpb.SubmitTransactionRequest{
				Command: &walletpb.SubmitTransactionRequest_BatchMarketInstructions{
					BatchMarketInstructions: &batch,
				},
			},
		)

		if err != nil {
			log.Printf("Error subitting the batch: %v", err)
		}

		// log.Printf("Batch market instructions: %v", batch.String())

	}
}

func SetLiquidityCommitment(walletClient *wallet.Client, dataClient *DataClient) {

	if market := dataClient.s.v[dataClient.c.LpMarket].GetMarket(); market != nil {
		asset := dataClient.s.v[dataClient.c.LpMarket].GetAsset(
			market.GetTradableInstrument().
				GetInstrument().
				GetFuture().
				GetSettlementAsset(),
		)

		// Determine LP commitment size
		commitmentAmountUSD, _ := decimal.NewFromString(dataClient.c.LpCommitmentSizeUSD)
		commitmentAmount := commitmentAmountUSD.Mul(decimal.NewFromInt(10).Pow(decimal.NewFromInt(int64(asset.Details.Decimals))))

		// Create LP submission
		lpSubmission := &commandspb.LiquidityProvisionSubmission{
			MarketId:         dataClient.c.LpMarket,
			CommitmentAmount: commitmentAmount.BigInt().String(),
			Fee:              "0.0001",
			Sells:            getLiquidityOrders(vegapb.Side_SIDE_SELL, dataClient),
			Buys:             getLiquidityOrders(vegapb.Side_SIDE_BUY, dataClient),
			Reference:        "Opportunities don't happen, you create them.",
		}

		// Submit transaction
		err := walletClient.SendTransaction(
			context.Background(), dataClient.c.WalletPubkey, &walletpb.SubmitTransactionRequest{
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

func getLiquidityOrders(side vegapb.Side, dataClient *DataClient) []*vegapb.LiquidityOrder {

	offset := 0.0015
	numOrders := 6

	sizeF := func(i int) decimal.Decimal {
		return decimal.NewFromInt(2).Pow(decimal.NewFromInt(int64(i)))
	}

	vegaBestBid, _ := decimal.NewFromString(dataClient.s.v[dataClient.c.LpMarket].GetMarketData().GetBestBidPrice())

	offsetF := func(i int) decimal.Decimal {
		if i == 1 {
	        return decimal.NewFromFloat(0.00125).Mul(vegaBestBid)
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

// func getOrderSubmission(d decimals, ourBestPrice int, vegaSpread, vegaRefPrice, binanceRefPrice, offset, targetVolume decimal.Decimal, side vegapb.Side, marketId string) []*commandspb.OrderSubmission {

// 	numOrders := 0
// 	totalOrderSizeUnits := float64(0)
// 	if marketId == "2c2ea995d7366e423be7604f63ce047aa7186eb030ecc7b77395eae2fcbffcc5" {
// 		numOrders = 5
// 		totalOrderSizeUnits = (math.Pow(float64(1.8), float64(numOrders+1)) - float64(1)) / float64(1.8-1)
// 		// totalOrderSizeUnits = int(math.Pow(float64(1.5), float64(numOrders)))
// 	} else if marketId == "074c929bba8faeeeba352b2569fc5360a59e12cdcbf60f915b492c4ac228b566" {
// 		numOrders = 4
// 		totalOrderSizeUnits = (math.Pow(float64(2), float64(numOrders+1)) - float64(1)) / float64(2-1)
// 		// totalOrderSizeUnits = int(math.Pow(float64(1.5), float64(numOrders)))
// 	} else {
// 		numOrders = 3
// 		totalOrderSizeUnits = (math.Pow(float64(1.9), float64(numOrders+1)) - float64(1)) / float64(1.9-1)
// 	}
// 	// numOrders := 3
// 	// totalOrderSizeUnits := 2*int(math.Pow(float64(1.6), float64(numOrders))) - 2
// 	orders := []*commandspb.OrderSubmission{}

// 	sizeF := func(i int) decimal.Decimal {
// 		return decimal.Max(
// 			targetVolume.Div(decimal.NewFromFloat(totalOrderSizeUnits).Mul(vegaRefPrice.Div(d.priceFactor))).Mul(decimal.NewFromFloat(1.9).Pow(decimal.NewFromInt(int64(i)))),
// 			decimal.NewFromInt(1).Div(d.positionFactor),
// 		)
// 	}
// 	if marketId == "2c2ea995d7366e423be7604f63ce047aa7186eb030ecc7b77395eae2fcbffcc5" {
// 		sizeF = func(i int) decimal.Decimal {
// 			return decimal.Max(
// 				targetVolume.Div(decimal.NewFromFloat(totalOrderSizeUnits).Mul(vegaRefPrice.Div(d.priceFactor))).Mul(decimal.NewFromFloat(1.8).Pow(decimal.NewFromInt(int64(i)))),
// 				decimal.NewFromInt(1).Div(d.positionFactor),
// 			)
// 		}
// 	} else if marketId == "074c929bba8faeeeba352b2569fc5360a59e12cdcbf60f915b492c4ac228b566" {
// 		sizeF = func(i int) decimal.Decimal {
// 			return decimal.Max(
// 				targetVolume.Div(decimal.NewFromFloat(totalOrderSizeUnits).Mul(vegaRefPrice.Div(d.priceFactor))).Mul(decimal.NewFromFloat(2).Pow(decimal.NewFromInt(int64(i)))),
// 				decimal.NewFromInt(1).Div(d.positionFactor),
// 			)
// 		}
// 	}

// 	priceF := func(i int) decimal.Decimal {

// 		if i == 1 && offset.IsZero() {
// 			// First order, push it to the front of the book
// 			log.Printf("Our best bid: %v", ourBestPrice)
// 			if vegaRefPrice.GreaterThan(decimal.NewFromInt(int64(ourBestPrice))) {
// 				log.Printf("Pushing bid to front of book")
// 				if vegaSpread.GreaterThan(decimal.NewFromInt(1)) {
// 					return vegaRefPrice.Div(d.priceFactor).Add(decimal.NewFromInt(1).Div(d.priceFactor))
// 				}
// 				return vegaRefPrice.Div(d.priceFactor)
// 			}
// 			return vegaRefPrice.Div(d.priceFactor)
// 		}

// 		return vegaRefPrice.Div(d.priceFactor).Mul(
// 			decimal.NewFromInt(1).Sub(decimal.NewFromInt(int64(i)).Mul(decimal.NewFromFloat(0.0009))).Sub(offset),
// 		)
// 	}

// 	if side == vegapb.Side_SIDE_SELL {
// 		priceF = func(i int) decimal.Decimal {

// 			if i == 1 && offset.IsZero() {
// 				// First order, push it to the front of the book
// 				log.Printf("Our best ask: %v", ourBestPrice)
// 				if vegaRefPrice.LessThan(decimal.NewFromInt(int64(ourBestPrice))) {
// 					log.Printf("Pushing ask to front of book")
// 					if vegaSpread.GreaterThan(decimal.NewFromInt(1)) {
// 						return vegaRefPrice.Div(d.priceFactor).Sub(decimal.NewFromInt(1).Div(d.priceFactor))
// 					}
// 					return vegaRefPrice.Div(d.priceFactor)
// 				}
// 				return vegaRefPrice.Div(d.priceFactor)
// 			}

// 			return vegaRefPrice.Div(d.priceFactor).Mul(
// 				decimal.NewFromInt(1).Add(decimal.NewFromInt(int64(i)).Mul(decimal.NewFromFloat(0.0009))).Add(offset),
// 			)
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

func getDecimals(market *vegapb.Market, asset *vegapb.Asset) decimals {
	return decimals{
		positionFactor: decimal.NewFromFloat(10).Pow(decimal.NewFromInt(market.PositionDecimalPlaces)),
		priceFactor:    decimal.NewFromFloat(10).Pow(decimal.NewFromInt(int64(market.DecimalPlaces))),
	}
}

func getEntryPriceAndVolume(d decimals, market *vegapb.Market, position *vegapb.Position) (volume, entryPrice decimal.Decimal) {

	if position == nil {
		return
	}

	volume = decimal.NewFromInt(position.OpenVolume)
	entryPrice, _ = decimal.NewFromString(position.AverageEntryPrice)

	return volume.Div(d.positionFactor), entryPrice.Div(d.priceFactor)
}

func getPubkeyBalance(marketId string, vega map[string]*VegaStore, pubkey, asset string, decimalPlaces int64) (d decimal.Decimal) {

	// marketId := maps.Keys(vega)[0]

	for _, acc := range vega[marketId].GetAccounts() {
		if acc.Owner != pubkey || acc.Asset != asset {
			continue
		}

		balance, _ := decimal.NewFromString(acc.Balance)
		d = d.Add(balance)
	}

	return d.Div(decimal.NewFromFloat(10).Pow(decimal.NewFromInt(decimalPlaces)))
}

func getOrderSubmission(d decimals, ourBestPrice int, vegaSpread, vegaRefPrice, binanceRefPrice, offset, targetVolume decimal.Decimal, side vegapb.Side, marketId string, riskParams *vegapb.LogNormalModelParams, tau float64) []*commandspb.OrderSubmission {

	firstOrderProbabilityOfTrading := decimal.NewFromFloat(0.85)

	refPrice, _ := vegaRefPrice.Div(d.priceFactor).Float64()

	firstPrice := findPriceByProbabilityOfTrading(firstOrderProbabilityOfTrading, side, refPrice, riskParams, tau)

	log.Printf("Calculated price: %v, Side: %v \n", firstPrice, side)

	orderSpacing := decimal.NewFromFloat(0.0011)

	numOrders := 6
	// totalOrderSizeUnits := (math.Pow(float64(2), float64(numOrders+1)) - float64(1)) / float64(2-1)
	orders := []*commandspb.OrderSubmission{}

	sizeF := func() decimal.Decimal {
		return decimal.Max(targetVolume.Div(decimal.NewFromInt(int64(numOrders)).Mul(vegaRefPrice.Div(d.priceFactor))), decimal.NewFromInt(1).Div(d.positionFactor))
	}

	log.Printf("targetVol; %v, vegaRefPrice: %v", targetVolume, vegaRefPrice)

	priceF := func(i int) decimal.Decimal {

		if i == 1 && offset.IsZero() {
			log.Printf("Our best bid: %v", ourBestPrice)
			return decimal.NewFromFloat(firstPrice)
		}
		return decimal.NewFromFloat(firstPrice).Mul(decimal.NewFromInt(1).Sub(decimal.NewFromInt(int64(i)).Mul(orderSpacing)).Sub(offset))

	}

	if side == vegapb.Side_SIDE_SELL {

		priceF = func(i int) decimal.Decimal {
			if i == 1 && offset.IsZero() {
				log.Printf("Our best ask: %v", ourBestPrice)
				return decimal.NewFromFloat(firstPrice)
			}
			return decimal.NewFromFloat(firstPrice).Mul(decimal.NewFromInt(1).Add(decimal.NewFromInt(int64(i)).Mul(orderSpacing)).Add(offset))
		}

	}

	for i := 1; i <= numOrders; i++ {
		orders = append(orders, &commandspb.OrderSubmission{
			MarketId: marketId,
			Price:    priceF(i).Mul(d.priceFactor).BigInt().String(),
			// Size:        sizeF(i).Mul(d.positionFactor).BigInt().Uint64(),
			Size:        sizeF().Mul(d.positionFactor).BigInt().Uint64(),
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

func findPriceByProbabilityOfTrading(probability decimal.Decimal, side vegapb.Side, refPrice float64, riskParams *vegapb.LogNormalModelParams, tau float64) (price float64) {

	log.Printf("prob: %v, refPrice: %v, tau: %v", probability, refPrice, tau)

	// Iterate through prices until we find a price that corresponds to our desired probability.

	// Get analytical distribution
	modelParams := riskmodelbs.ModelParamsBS{
		Mu:    riskParams.Mu,
		R:     riskParams.R,
		Sigma: riskParams.Sigma,
	}

	dist := modelParams.GetProbabilityDistribution(refPrice, tau)

	// Get price range from distribution
	minPrice, maxPrice := pd.PriceRange(dist, probability.InexactFloat64())

	log.Printf("minPrice: %v, maxPrice: %v", minPrice, maxPrice)

	price = refPrice
	calculatedProb := float64(1)

	for calculatedProb > probability.InexactFloat64() {
		if side == vegapb.Side_SIDE_BUY {
			price = price - float64(price*0.0005)
		} else {
			price = price + float64(price*0.0005)
		}
		calculatedProb = getProbabilityOfTradingForOrder(riskParams.Mu, riskParams.Sigma, tau, minPrice, maxPrice, price, refPrice, side)
		log.Printf("Side: %v, Price: %v, Last calculated probability: %v, probability: %v", side, price, calculatedProb, probability)
	}

	log.Printf("Side: %v, Last calculated probability: %v", side, calculatedProb)

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
