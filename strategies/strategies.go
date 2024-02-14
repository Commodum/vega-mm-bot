package strats

import (
	"sync"
	"vega-mm/metrics"
	"vega-mm/stores"

	vegapb "code.vegaprotocol.io/vega/protos/vega"
	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"
	"github.com/shopspring/decimal"
)

type OptsType interface {
	~*MartingaleOpts | ~*AggressiveOpts
}

type StrategyOpts[T OptsType] struct {
	General  *GeneralOpts
	Specific T
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

type syncPubKeyBalance struct {
	mu    *sync.RWMutex
	value decimal.Decimal
}

type Strategy interface {
	SetTxDataChan(chan *commandspb.InputData)

	GetAgentKeyPairIndex() uint64

	GetVegaMarketId() string
	GetBinanceMarketTicker() string

	GetVegaStore() *stores.VegaStore
	GetBinanceStore() *stores.BinanceStore

	GetMarketSettlementAsset() string

	SetVegaDecimals(positionDecimals, priceDecimals, assetDecimals int64)
	GetVegaDecimals() *decimals

	UsesLiquidityCommitment() bool
	GetTargetObligationVolume() decimal.Decimal
	SubmitLiquidityCommitment()
	AmendLiquidityCommitment()
	CancelLiquidityCommitment()

	RunStrategy(chan *metrics.MetricsEvent)

	SetAgentPubKey(string)
	GetAgentPubKey() string
	SetAgentPubKeyBalance(decimal.Decimal)
	GetAgentPubKeyBalance() decimal.Decimal

	GetOurBestBidAndAsk([]*vegapb.Order) (decimal.Decimal, decimal.Decimal)

	// GetOrderSubmission(decimal.Decimal, decimal.Decimal, decimal.Decimal, decimal.Decimal, decimal.Decimal, decimal.Decimal, vegapb.Side, *vegapb.LogNormalRiskModel, *vegapb.LiquiditySLAParameters) []*commandspb.OrderSubmission

	GetEntryPriceAndVolume() (decimal.Decimal, decimal.Decimal)
}

type decimals struct {
	positionFactor decimal.Decimal
	priceFactor    decimal.Decimal
	assetFactor    decimal.Decimal
}
