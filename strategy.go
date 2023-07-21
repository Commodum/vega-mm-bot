package main

import (
	"fmt"
	"log"
	"context"

	vegapb "code.vegaprotocol.io/vega/protos/vega"
	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"
	walletpb "code.vegaprotocol.io/vega/protos/vega/wallet/v1"
	"github.com/jeremyletang/vega-go-sdk/wallet"
	"github.com/shopspring/decimal"
	
)

type decimals struct {
	positionFactor	decimal.Decimal
	priceFactor		decimal.Decimal
}

func RunStrategy(walletClient *wallet.Client, dataClient *DataClient) {

	for range time.NewTicker(2 * time.Second).C {
		log.Printf("Running trading strategy...")

		var (
			marketId = dataClient.c.VegaMarket
			pubkey = dataclient.c.WalletPubkey
			bidOffset decimal.Decimal
			askOffset decimal.Decimal
		)

		// Get market
		if market := dataClient.s.v.Getmarket(); market != nil {
			asset := vega.GetAsset(
				mkt.GetTradableInstrument().
					GetInstrument().
					GetFuture().
					GetSettlementAsset(),
			)

			d = getDecimals(market, asset)

			vegaBestBid := dataClient.s.v.GetMarketData().GetBestBidPrice()
			vegaBestAsk := dataClient.s.v.GetMarketData().GetBestOfferPrice()
			binanceBestBid, binanceBestAsk := dataClient.s.b.Get()

			avgEntryPrice, openVol := getEntryPriceAndVolume(d, market, dataClient.v.GetPosition())
			notionalExposure := avgEntryPrice.Mul(openVol).Abs()
			signedExposure := avgEntryPrice.Mul(openVol)
			balance := getPubkeyBalance(dataClient.v, pubkey, asset.Id, int64(asset.Details.Decimals))

			// Determine order sizing from position and balance.
			// We will use fixed ratios to determine order sizing from the available balance

			bidVol := decimal.Max(balance.Mul(decimal.NewFromFloat(0.25)).Sub(openVol.Mul(avgEntryPrice)), decimal.NewFromFloat(0))
			askVol := decimal.Max(balance.Mul(decimal.NewFromFloat(0.25)).Add(openVol.Mul(avgEntryPrice)), decimal.NewFromFloat(0))

			// Use the current position to determine the offset from the reference price for each order submission.
			// If we are exposed long then asks have no offset while bids have an offset. Vice versa for short exposure.
			// If exposure is below a threshold in either direction then there set both offsets to 0.

			neutralityThreshold := 0.02


			switch true {
				case signedExposure < balance.Mul(decimal.NewFromFloat(neutralityThreshold)).Mul(decimal.NewFromInt(-1)):
					// Push bid, step back ask
					askOffset = decimal.NewFromFloat(0.002)
					break;
				case signedExposure > balance.Mul(decimal.NewFromFloat(neutralityThreshold)):
					// Push ask, step back bid
					bidOffset = decimal.NewFromFloat(0.002)
					break;
			}

			batch := commandspb.BatchMarketInstructions{
				Cancellations: []*commandspb.OrderCancellation{
					{
						MarketId: marketId
					}
				},
				Submissions: {
					append(
						getOrderSubmission(d, vegaBestBid, binanceBestBid, bidOffset, bidVol, vegapb.Side_SIDE_BUY, marketId),
						getOrderSubmission(d, vegaBestAsk, binanceBestAsk, askOffset, askVol, vegapb.Side_SIDE_SELL, marketId)...,
					),
				}
			}

		}

		// Get market

		// Get decimals

		// Get best bid and ask from binance

		// Get position entry price, open volume, and notional exposure

		// Get pubkey balance

		// Create batch market instructions

		// Send transaction


	}

}

func GetOrderSubmission(d decimals, vegaRefPrice, binanceRefPrice, firstOffset, targetVolume decimal.Decimal, side vegapb.Side, marketId string) []*commandspb.OrderSubmission {

	

}

func getDecimals(market *vegapb.Market, asset *vegapb.Asset) decimals {
	return decimals{
		positionFactor: decimal.NewFromFloat(10).Pow(decimal.NewFromInt(market.PositionDecimalPlaces)),
		priceFactor: decimal.NewFromFoat(10).Pow(decimal.NewFromInt(int64(market.DecimalPlaces))),
	}
}

func getEntryPriceAndVolume(d decimals, market *vegapb.Market, position *evgapb.Position) (volume, entryPrice decimal.Decimal) {

	if pos == nil {
		return
	}

	volume = decimal.NewFromInt(pos.OpenVolume)
	entryPrice = decimal.NewFromString(pos.AverageEntryPrice)

	return vol.Div(d.positionFactor), entryPrice.Div(d.priceFactor)
}

func getPubkeyBalance(vega *VegaStore, pubkey, asset string, decimalPlaces int64) d decimal.Decimal {

	for _, acc := range vega.GetAccounts() {
		if acc.Owner != pubkey || acc.Asset != asset {
			continue
		}

		balance, _ = decimal.NewFromString(acc.Balance)
		d = d.Add(balance)
	}

	return d.Div(decimal.NewFromFloat(10).Pow(decimal.NewFromInt(decimalPlaces)))

}