package trading

import (
	"log"
	"vega-mm/metrics"
	// "vega-mm/pow"
	"vega-mm/strategies"
	"vega-mm/wallets"

	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"
	"github.com/shopspring/decimal"
	"golang.org/x/exp/maps"
)

type Agent struct {
	pubkey     string
	apiToken   string
	balance    decimal.Decimal
	strategies map[string]strats.Strategy
	txDataCh   chan *commandspb.InputData
	signer     *wallets.VegaSigner
	// config     *Config
	// metricsCh  chan *MetricsState
}

// func NewAgent(wallet *wallets.EmbeddedVegaWallet, keyPairIndex uint64, txBroadcastCh chan *commandspb.Transaction) *Agent {
func NewAgent(keyPair *wallets.VegaKeyPair, txBroadcastCh chan *commandspb.Transaction) *Agent {
	signer := wallets.NewVegaSigner(keyPair, txBroadcastCh)
	agent := &Agent{
		pubkey:     keyPair.PubKey(),
		strategies: map[string]strats.Strategy{},

		// We need to get the gRPC addresses to the clients

		signer: signer,
		// config:    config,
		// metricsCh: make(chan *MetricsState),
	}
	return agent
}

// Adds the strategy to the agents internal map
func (a *Agent) RegisterStrategy(strat strats.Strategy) {

	vegaMarket := strat.GetVegaMarketId()

	if _, ok := a.strategies[vegaMarket]; ok {
		log.Printf("Strategy already registered for vega market. Ignoring...")
		return
	}

	strat.SetTxDataChan(a.signer.GetInChan())
	a.strategies[vegaMarket] = strat
}

// Loads the vega decimals for each strategy. Must be called after connecting to
// and receiving initial data from a data-node.
func (a *Agent) LoadVegaDecimals() {
	for _, strat := range maps.Values(a.strategies) {
		market := strat.GetVegaStore().GetMarket()
		asset := strat.GetVegaStore().GetAsset(market.GetTradableInstrument().GetInstrument().GetPerpetual().SettlementAsset)

		strat.SetVegaDecimals(market.PositionDecimalPlaces, int64(market.DecimalPlaces), int64(asset.Details.Decimals))
	}

}

func (agent *Agent) UpdateLiquidityCommitment(strat strats.Strategy) {
	if !strat.UsesLiquidityCommitment() {
		log.Printf("Strategy does not utilize a liquidity commitment.")
		return
	}

	lpCommitment := strat.GetVegaStore().GetLiquidityProvision()
	targetObligationVolume := strat.GetTargetObligationVolume()

	switch true {
	case (lpCommitment != nil && targetObligationVolume.IsZero()):
		strat.CancelLiquidityCommitment()
	case (lpCommitment == nil && !targetObligationVolume.IsZero()):
		strat.SubmitLiquidityCommitment()
	case (lpCommitment != nil && !targetObligationVolume.IsZero()):
		strat.AmendLiquidityCommitment()
	default:
		return
	}
}

func (a *Agent) RunStrategies(metricsCh chan *metrics.MetricsState) {
	for _, strat := range maps.Values(a.strategies) {
		go strat.RunStrategy(metricsCh)
	}
}
