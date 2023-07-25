package main

import (
	"context"
	"log"
	"math"
	"time"

	vegapb "code.vegaprotocol.io/vega/protos/vega"
	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"
	walletpb "code.vegaprotocol.io/vega/protos/vega/wallet/v1"
	"github.com/jeremyletang/vega-go-sdk/wallet"
	"github.com/shopspring/decimal"
)

type decimals struct {
	positionFactor decimal.Decimal
	priceFactor    decimal.Decimal
}

func RunStrategy(walletClient *wallet.Client, dataClient *DataClient) {

	for range time.NewTicker(1500 * time.Millisecond).C {
		log.Printf("Executing strategy...")

		var (
			marketId  = dataClient.c.VegaMarket
			pubkey    = dataClient.c.WalletPubkey
			bidOffset = decimal.NewFromInt(0)
			askOffset = decimal.NewFromInt(0)
		)

		// Get market
		if market := dataClient.s.v.GetMarket(); market != nil {
			asset := dataClient.s.v.GetAsset(
				market.GetTradableInstrument().
					GetInstrument().
					GetFuture().
					GetSettlementAsset(),
			)

			d := getDecimals(market, asset)

			vegaBestBid, _ := decimal.NewFromString(dataClient.s.v.GetMarketData().GetBestBidPrice())
			vegaBestAsk, _ := decimal.NewFromString(dataClient.s.v.GetMarketData().GetBestOfferPrice())
			vegaSpread := vegaBestAsk.Sub(vegaBestBid)
			binanceBestBid, binanceBestAsk := dataClient.s.b.Get()

			log.Printf("Vega best bid: %v, Vega best ask: %v", vegaBestBid, vegaBestAsk)
			log.Printf("Vega spread: %v", vegaSpread)

			openVol, avgEntryPrice := getEntryPriceAndVolume(d, market, dataClient.s.v.GetPosition())
			notionalExposure := avgEntryPrice.Mul(openVol).Abs()
			signedExposure := avgEntryPrice.Mul(openVol)
			balance := getPubkeyBalance(dataClient.s.v, pubkey, asset.Id, int64(asset.Details.Decimals))

			// Determine order sizing from position and balance.
			bidVol := decimal.Max(balance.Mul(decimal.NewFromFloat(0.6)).Sub(decimal.Max(openVol.Mul(avgEntryPrice), decimal.NewFromFloat(0))), decimal.NewFromFloat(0))
			askVol := decimal.Max(balance.Mul(decimal.NewFromFloat(0.6)).Add(decimal.Min(openVol.Mul(avgEntryPrice), decimal.NewFromFloat(0))), decimal.NewFromFloat(0))

			log.Printf("Binance best bid: %v, Binance best ask: %v", binanceBestBid, binanceBestAsk)
			log.Printf("Open volume: %v, entry price: %v, notional exposure: %v", openVol, avgEntryPrice, notionalExposure)
			log.Printf("Bid volume: %v, ask volume: %v", bidVol, askVol)

			// Use the current position to determine the offset from the reference price for each order submission.
			// If we are exposed long then asks have no offset while bids have an offset. Vice versa for short exposure.
			// If exposure is below a threshold in either direction then there set both offsets to 0.

			rebalanceThreshold := 0.08
			// rebalanceThresholdLong := 0.08
			// rebalanceThresholdShort := 0.08

			switch true {
			case signedExposure.LessThan(balance.Mul(decimal.NewFromFloat(rebalanceThreshold)).Mul(decimal.NewFromInt(-1))):
				// Push bid, step back ask
				askOffset = decimal.NewFromFloat(0.0015)
				break
			case signedExposure.GreaterThan(balance.Mul(decimal.NewFromFloat(rebalanceThreshold))):
				// Push ask, step back bid
				bidOffset = decimal.NewFromFloat(0.0015)
				break
			}

			batch := commandspb.BatchMarketInstructions{
				Cancellations: []*commandspb.OrderCancellation{
					{
						MarketId: marketId,
					},
				},
				Submissions: append(
					getOrderSubmission(d, vegaSpread, vegaBestBid, binanceBestBid, bidOffset, bidVol, vegapb.Side_SIDE_BUY, marketId),
					getOrderSubmission(d, vegaSpread, vegaBestAsk, binanceBestAsk, askOffset, askVol, vegapb.Side_SIDE_SELL, marketId)...,
				),
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
}

func getOrderSubmission(d decimals, vegaSpread, vegaRefPrice, binanceRefPrice, offset, targetVolume decimal.Decimal, side vegapb.Side, marketId string) []*commandspb.OrderSubmission {

	numOrders := 3
	totalOrderSizeUnits := 2*int(math.Pow(float64(1.9), float64(numOrders))) - 2
	orders := []*commandspb.OrderSubmission{}

	sizeF := func(i int) decimal.Decimal {
		return decimal.Max(
			targetVolume.Div(decimal.NewFromInt(int64(totalOrderSizeUnits)).Mul(binanceRefPrice)).Mul(decimal.NewFromFloat(1.9).Pow(decimal.NewFromInt(int64(i+1)))),
			decimal.NewFromInt(1).Div(d.positionFactor),
		)
	}

	priceF := func(i int) decimal.Decimal {

		if i == 1 && offset.IsZero() {
			// First order, push it to the front of the book
			log.Printf("Pushing bid to front of book")
			if vegaSpread.GreaterThan(decimal.NewFromInt(1)) {
				return vegaRefPrice.Div(d.priceFactor).Add(decimal.NewFromInt(1).Div(d.priceFactor))
			}
			return vegaRefPrice.Div(d.priceFactor)
		}

		return binanceRefPrice.Mul(
			decimal.NewFromInt(1).Sub(decimal.NewFromInt(int64(i)).Mul(decimal.NewFromFloat(0.001))).Sub(offset),
		)
	}

	if side == vegapb.Side_SIDE_SELL {
		priceF = func(i int) decimal.Decimal {

			if i == 1 && offset.IsZero() {
				// First order, push it to the front of the book
				log.Printf("Pushing ask to front of book")
				if vegaSpread.GreaterThan(decimal.NewFromInt(1)) {
					return vegaRefPrice.Div(d.priceFactor).Sub(decimal.NewFromInt(1).Div(d.priceFactor))
				}
				return vegaRefPrice.Div(d.priceFactor)
			}

			return binanceRefPrice.Mul(
				decimal.NewFromInt(1).Add(decimal.NewFromInt(int64(i)).Mul(decimal.NewFromFloat(0.001))).Add(offset),
			)
		}
	}

	for i := 1; i <= numOrders; i++ {
		orders = append(orders, &commandspb.OrderSubmission{
			MarketId:    marketId,
			Price:       priceF(i).Mul(d.priceFactor).BigInt().String(),
			Size:        sizeF(i).Mul(d.positionFactor).BigInt().Uint64(),
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

func getPubkeyBalance(vega *VegaStore, pubkey, asset string, decimalPlaces int64) (d decimal.Decimal) {

	for _, acc := range vega.GetAccounts() {
		if acc.Owner != pubkey || acc.Asset != asset {
			continue
		}

		balance, _ := decimal.NewFromString(acc.Balance)
		d = d.Add(balance)
	}

	return d.Div(decimal.NewFromFloat(10).Pow(decimal.NewFromInt(decimalPlaces)))

}
