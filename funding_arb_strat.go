package main

import (
	"context"
	"log"
	"time"

	dataApiPb "code.vegaprotocol.io/vega/protos/data-node/api/v2"
	vegapb "code.vegaprotocol.io/vega/protos/vega"

	// apipb "code.vegaprotocol.io/vega/protos/vega/api/v1"
	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"
	"github.com/shopspring/decimal"
)

type kuCoinClient struct {
}

type FundingArbStrat struct {
	marketId string

	d *decimals

	vegaStore *VegaStore

	agent *agent
}

func (f *FundingArbStrat) Init() {
	f.agent.vegaClient.vegaMarkets = append(f.agent.vegaClient.vegaMarkets, f.marketId)
}

func (f *FundingArbStrat) GetDecimals() {
	market := f.vegaStore.GetMarket()
	asset := f.vegaStore.GetAsset(market.GetTradableInstrument().GetInstrument().GetPerpetual().SettlementAsset)
	f.d = &decimals{
		positionFactor: decimal.NewFromInt(10).Pow(decimal.NewFromInt(market.PositionDecimalPlaces)),
		priceFactor:    decimal.NewFromInt(10).Pow(decimal.NewFromInt(int64(market.DecimalPlaces))),
		assetFactor:    decimal.NewFromInt(10).Pow(decimal.NewFromInt(int64(asset.Details.Decimals))),
	}
}

func (f *FundingArbStrat) Run() {

	// Intensive Workflow:
	//	- Call Ethereum RPC to get chainlink oracle data
	//	- Determine if oracle data is about to update
	//	- Get funding rates on Vega and Kucoin
	// 	- Determine trade direction from funding rates
	//	- Estimate fees
	//	- Determine viable order size
	//	- Get orders for both exchanges
	// 	- Calculate slippage for order size
	//	-

	// Easy workflow:
	//	- Get funding rate on Vega
	//	- Place tiny trade to update funding rate
	//	- If fundingRate > threshold && timeUntilFunding < 3s --> Enter trade
	//	- Close position after funding.
	//

	const (
		sizeUsd          = 500
		fundingThreshold = 0.15
	)

	for range time.NewTicker(time.Millisecond * 750).C {

		var (
			perpetual              = f.vegaStore.GetMarket().TradableInstrument.Instrument.GetPerpetual()
			settlementTimeTriggers = perpetual.GetDataSourceSpecForSettlementSchedule().GetData().GetInternal().GetTimeTrigger().Triggers
			// fundingIntervalStart   = *settlementTimeTriggers[0].Initial * 1e9
			fundingIntervalNanos = settlementTimeTriggers[0].Every * 1e9
			position             = f.vegaStore.GetPosition()
		)

		vegaTimeNow := f.vegaStore.GetMarketData().Timestamp

		if vegaTimeNow%fundingIntervalNanos <= fundingIntervalNanos-(5*1e9) {
			// If more than 5s remaining before funding, close open position.
			f.ClosePosition(position)

			vegaFundingRate := f.EstimateVegaFundingRate()
			_ = vegaFundingRate

			continue
		} else if vegaTimeNow%fundingIntervalNanos >= fundingIntervalNanos-(5*1e9) {
			vegaFundingRate := f.EstimateVegaFundingRate()

			switch true {
			case vegaFundingRate > 0 && vegaFundingRate >= fundingThreshold:
				f.OpenPosition(vegapb.Side_SIDE_SELL, sizeUsd, position)
			case vegaFundingRate < 0 && vegaFundingRate <= -fundingThreshold:
				f.OpenPosition(vegapb.Side_SIDE_BUY, sizeUsd, position)
			default:
				continue
			}

		}

	}

}

func (f *FundingArbStrat) EstimateVegaFundingRate() float64 {

	// Can't just get the estimate from Vega so let's calculate it!

	// Call funding periods and data points RPCs
	first := int32(3)
	fundingPeriodsReq := &dataApiPb.ListFundingPeriodsRequest{
		MarketId: f.marketId,
		Pagination: &dataApiPb.Pagination{
			First: &first,
		},
	}

	res, err := f.agent.vegaClient.svc.ListFundingPeriods(context.Background(), fundingPeriodsReq)
	if err != nil {
		log.Printf("Failed to list funding periods from datanode: %v", err)
		f.agent.vegaClient.handleGrpcReconnect()
		return 0
	}

	edges := res.FundingPeriods.Edges
	log.Printf("Funding Periods: %+v", edges)

	seq := uint64(edges[0].Node.Seq)
	dataPointsReq := &dataApiPb.ListFundingPeriodDataPointsRequest{
		MarketId: f.marketId,
		Seq:      &seq,
	}

	dataPointsRes, err := f.agent.vegaClient.svc.ListFundingPeriodDataPoints(context.Background(), dataPointsReq)
	if err != nil {
		log.Printf("Failed to list funding period datapoints from datanode: %v", err)
		f.agent.vegaClient.handleGrpcReconnect()
		return 0
	}

	dataPointsEdges := dataPointsRes.FundingPeriodDataPoints.Edges
	log.Printf("Funding period data points: %+v", dataPointsEdges)

	// Sort data points by timestamp and type, get twaps

	// Let's also validate our method first before using it.
	//	- Get historical funding periods
	//	- Get historical funding data-points
	//	- Calculate funding rate
	// 	- Compare to historical funding rate

	return 0.

}

func (f *FundingArbStrat) OpenPosition(side vegapb.Side, sizeUSD int64, currentPos *vegapb.Position) {

	if currentPos != nil && currentPos.OpenVolume != 0 {
		return
	}

}

func (f *FundingArbStrat) ClosePosition(pos *vegapb.Position) {

	if pos == nil || pos.OpenVolume == 0 {
		return
	}

	var side vegapb.Side
	if pos.OpenVolume > 0 {
		side = vegapb.Side_SIDE_SELL
	} else if pos.OpenVolume < 0 {
		side = vegapb.Side_SIDE_BUY
	}

	order := &commandspb.OrderSubmission{
		MarketId:    f.marketId,
		Size:        decimal.NewFromInt(pos.GetOpenVolume()).Mul(f.d.positionFactor).Abs().BigInt().Uint64(),
		Side:        side,
		TimeInForce: vegapb.Order_TIME_IN_FORCE_IOC,
		ReduceOnly:  true,
		Type:        vegapb.Order_TYPE_MARKET,
		Reference:   "ref",
	}

	inputData := &commandspb.InputData{
		Command: &commandspb.InputData_OrderSubmission{
			OrderSubmission: order,
		},
	}

	tx := f.agent.signer.BuildTx(f.agent.pubkey, inputData)

	f.agent.signer.SubmitTx(tx)
}
