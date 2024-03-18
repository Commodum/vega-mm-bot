package strats

import (
	"log"
	"math"
	"strconv"
	"vega-mm/stores"

	vegapb "code.vegaprotocol.io/vega/protos/vega"
	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"
	"github.com/shopspring/decimal"
)

type BaseStrategy struct {
	d                  *decimals
	agentPubKey        string
	agentPubKeyBalance syncPubKeyBalance

	*GeneralOpts

	vegaStore    *stores.VegaStore
	binanceStore *stores.BinanceStore

	txDataCh chan *commandspb.InputData
}

func NewBaseStrategy(opts *GeneralOpts) *BaseStrategy {

	bs := &BaseStrategy{
		GeneralOpts: opts,

		vegaStore:    stores.NewVegaStore(opts.VegaMarketId),
		binanceStore: stores.NewBinanceStore(opts.BinanceMarket),
	}

	if opts.NoBinance {
		bs.binanceStore = nil
	}

	return bs
}

func (strat *BaseStrategy) SetTxDataChan(ch chan *commandspb.InputData) {
	strat.txDataCh = ch
}

func (strat *BaseStrategy) GetAgentKeyPairIndex() uint64 {
	return strat.AgentKeyPairIdx
}

func (strat *BaseStrategy) GetVegaMarketId() string {
	return strat.VegaMarketId
}

func (strat *BaseStrategy) GetBinanceMarketTicker() string {
	return strat.BinanceMarket
}

func (strat *BaseStrategy) GetVegaStore() *stores.VegaStore {
	return strat.vegaStore
}

func (strat *BaseStrategy) GetBinanceStore() *stores.BinanceStore {
	return strat.binanceStore
}

// TODO: Replace this with switch
func (strat *BaseStrategy) GetMarketSettlementAsset() string {

	instrument := strat.GetVegaStore().GetMarket().GetTradableInstrument().GetInstrument()

	switch instrument.GetProduct().(type) {
	case *vegapb.Instrument_Future:
		return instrument.GetFuture().SettlementAsset
	case *vegapb.Instrument_Perpetual:
		return instrument.GetPerpetual().SettlementAsset
	default:
		panic("product not implemented.")
	}

}

func (strat *BaseStrategy) SetVegaDecimals(positionDecimals, priceDecimals, assetDecimals int64) {
	strat.d = &decimals{
		positionFactor: decimal.NewFromInt(10).Pow(decimal.NewFromInt(positionDecimals)),
		priceFactor:    decimal.NewFromInt(10).Pow(decimal.NewFromInt(priceDecimals)),
		assetFactor:    decimal.NewFromInt(10).Pow(decimal.NewFromInt(assetDecimals)),
	}
}

func (strat *BaseStrategy) GetVegaDecimals() *decimals {
	return strat.d
}

func (strat *BaseStrategy) UsesLiquidityCommitment() bool {
	return strat.LiquidityCommitment
}

func (strat *BaseStrategy) GetTargetObligationVolume() decimal.Decimal {
	return strat.TargetObligationVolume
}

func (strat *BaseStrategy) SubmitLiquidityCommitment() {

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

func (strat *BaseStrategy) AmendLiquidityCommitment() {
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
	}
}

func (strat *BaseStrategy) CancelLiquidityCommitment() {
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
func (s *BaseStrategy) SetAgentPubKey(pubkey string) {
	s.agentPubKey = pubkey
}

// No lock because this value should be written once only.
func (s *BaseStrategy) GetAgentPubKey() string {
	return s.agentPubKey
}

func (s *BaseStrategy) SetAgentPubKeyBalance(balance decimal.Decimal) {
	s.agentPubKeyBalance.mu.Lock()
	defer s.agentPubKeyBalance.mu.Unlock()
	s.agentPubKeyBalance.value = balance
}

func (s *BaseStrategy) GetAgentPubKeyBalance() decimal.Decimal {
	s.agentPubKeyBalance.mu.RLock()
	defer s.agentPubKeyBalance.mu.RUnlock()
	return s.agentPubKeyBalance.value
}

func (strat *BaseStrategy) GetOurBestBidAndAsk(liveOrders []*vegapb.Order) (decimal.Decimal, decimal.Decimal) {
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

func (strat *BaseStrategy) GetEntryPriceAndVolume() (volume, entryPrice decimal.Decimal) {

	if strat.vegaStore.GetPosition() == nil {
		return
	}

	volume = decimal.NewFromInt(strat.vegaStore.GetPosition().GetOpenVolume())
	entryPrice, _ = decimal.NewFromString(strat.vegaStore.GetPosition().GetAverageEntryPrice())

	return volume.Div(strat.d.positionFactor), entryPrice.Div(strat.d.priceFactor)
}

func (strat *BaseStrategy) GetPubkeyBalance(assetId string) (b decimal.Decimal) {

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
