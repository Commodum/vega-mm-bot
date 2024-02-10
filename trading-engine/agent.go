package trading

import (
	"log"
	"vega-mm/metrics"
	"vega-mm/pow"

	// "vega-mm/pow"
	"vega-mm/strategies"
	"vega-mm/wallets"

	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"
	"github.com/shopspring/decimal"
	"golang.org/x/exp/maps"
)

type Agent struct {
	index      uint64
	pubkey     string
	apiToken   string
	balance    decimal.Decimal
	strategies map[string]strats.Strategy
	txDataCh   chan *commandspb.InputData
	signer     *wallets.VegaSigner
	// config     *Config
	// metricsCh  chan *MetricsState
}

type Position struct {
	Venue      string
	Ticker     string
	OpenVolume decimal.Decimal
	EntryPrice decimal.Decimal
}

type AgentBalance struct {
	Venue   string
	Asset   string
	Balance decimal.Decimal
}

type AgentData struct {
	Positions []*Position
	Balances  []*AgentBalance
}

// func NewAgent(wallet *wallets.EmbeddedVegaWallet, keyPairIndex uint64, txBroadcastCh chan *commandspb.Transaction) *Agent {
func NewAgent(keyPair *wallets.VegaKeyPair, txBroadcastCh chan *commandspb.Transaction) *Agent {
	signer := wallets.NewVegaSigner(keyPair, txBroadcastCh)
	agent := &Agent{
		index:      keyPair.Index(),
		pubkey:     keyPair.PubKey(),
		strategies: map[string]strats.Strategy{},

		// We need to get the gRPC addresses to the clients

		signer: signer,
		// config:    config,
		// metricsCh: make(chan *MetricsState),
	}
	return agent
}

func (a *Agent) GetIndex() uint64 {
	return a.index
}

// Adds the strategy to the agents internal map
func (a *Agent) RegisterStrategy(strat strats.Strategy) {

	vegaMarket := strat.GetVegaMarketId()

	if _, ok := a.strategies[vegaMarket]; ok {
		log.Printf("Strategy already registered for vega market. Ignoring...")
		return
	}

	strat.SetAgentPubKey(a.pubkey)
	strat.GetVegaStore().SetAgentPubKey(a.pubkey)

	strat.SetTxDataChan(a.signer.GetInChan())
	a.strategies[vegaMarket] = strat
}

// Loads the vega decimals for each strategy. Must be called after connecting to
// and receiving initial data from a data-node.
func (a *Agent) LoadVegaDecimals() {
	log.Printf("Loading Vega decimals.")
	for _, strat := range maps.Values(a.strategies) {
		market := strat.GetVegaStore().GetMarket()
		asset := strat.GetVegaStore().GetAsset(market.GetTradableInstrument().GetInstrument().GetPerpetual().SettlementAsset)

		strat.SetVegaDecimals(market.PositionDecimalPlaces, int64(market.DecimalPlaces), int64(asset.Details.Decimals))
	}

}

func (agent *Agent) UpdateLiquidityCommitments() {
	for _, strat := range agent.strategies {
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
}

func (a *Agent) RunStrategies(metricsCh chan *metrics.MetricsEvent) {
	for _, strat := range maps.Values(a.strategies) {
		go strat.RunStrategy(metricsCh)
	}
}

func (a *Agent) GetStrategies() []strats.Strategy {
	s := []strats.Strategy{}
	for _, strat := range maps.Values(a.strategies) {
		s = append(s, strat)
	}
	return s
}

func (a *Agent) GetPowStore() *pow.PowStore {
	return a.signer.GetPowStore()
}

func (a *Agent) GetPositions() (p []*Position) {

	for _, strat := range a.strategies {
		openVol, entryPrice := strat.GetEntryPriceAndVolume()
		if openVol.IsZero() {
			continue
		}
		p = append(p, &Position{
			Venue:      "Vega",
			Ticker:     strat.GetBinanceMarketTicker(),
			OpenVolume: openVol,
			EntryPrice: entryPrice,
		})
	}

	return
}

func (a *Agent) GetBalances() (b []*AgentBalance) {

	type void struct{}
	assets := map[string]void{}

	for _, strat := range a.strategies {
		settlementAsset := strat.GetMarketSettlementAsset()
		if _, ok := assets[settlementAsset]; ok {
			continue
		}

		assets[settlementAsset] = void{}

		balance := strat.GetAgentPubKeyBalance()

		b = append(b, &AgentBalance{
			Venue:   "Vega",
			Asset:   "USDT", // We only have USDT markets for now
			Balance: balance,
		})
	}

	return
}
