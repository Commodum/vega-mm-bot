package trading

import (
	"log"
	"sync"
	"vega-mm/metrics"
	"vega-mm/pow"

	// "vega-mm/pow"
	"vega-mm/strategies"
	"vega-mm/wallets"

	vegapb "code.vegaprotocol.io/vega/protos/vega"
	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"
	"github.com/shopspring/decimal"
	"golang.org/x/exp/maps"
)

type Agent struct {
	mu sync.RWMutex

	index      uint64
	pubkey     string
	apiToken   string
	balance    decimal.Decimal
	strategies map[string]strats.Strategy
	txDataCh   chan *commandspb.InputData
	signer     *wallets.VegaSigner
	data       *AgentData
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
	Venue     string
	AssetName string
	AssetId   string
	Balance   decimal.Decimal
}

type AgentData struct {
	Positions []*Position
	Balances  []*AgentBalance
}

// func NewAgent(wallet *wallets.EmbeddedVegaWallet, keyPairIndex uint64, txBroadcastCh chan *commandspb.Transaction) *Agent {
func NewAgent(keyPair *wallets.VegaKeyPair, txBroadcastCh chan *commandspb.Transaction) *Agent {
	signer := wallets.NewVegaSigner(keyPair, txBroadcastCh)
	agent := &Agent{
		mu: sync.RWMutex{},

		index:      keyPair.Index(),
		pubkey:     keyPair.PubKey(),
		strategies: map[string]strats.Strategy{},
		signer:     signer,

		data: &AgentData{
			Positions: []*Position{},
			Balances:  []*AgentBalance{},
		},

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
		instrument := market.GetTradableInstrument().GetInstrument()

		var settlementAsset string
		switch instrument.GetProduct().(type) {
		case *vegapb.Instrument_Future:
			settlementAsset = instrument.GetFuture().SettlementAsset
		case *vegapb.Instrument_Perpetual:
			settlementAsset = instrument.GetPerpetual().SettlementAsset
		case *vegapb.Instrument_Spot:
			panic("We do not trade spot right now.")
		}

		asset := strat.GetVegaStore().GetAsset(settlementAsset)
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

		// TODO: Investiage why this func doesn't seem to work...
		log.Printf("LP Commitment: %+v", lpCommitment)
		log.Printf("Target Obligation Volume: %d", targetObligationVolume)

		continue

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
		// if strat.GetVegaMarketId() != "e63a37edae8b74599d976f5dedbf3316af82579447f7a08ae0495a021fd44d13" || strat.GetVegaMarketId() != "4e9081e20e9e81f3e747d42cb0c9b8826454df01899e6027a22e771e19cc79fc" {
		// 	strat.CancelLiquidityCommitment()
		// }

		////// EGLPUSDT LP Cancellation //////
		// if strat.GetVegaMarketId() == "fc37a1eedb6e57b86823e2fc42480a0b9236aea556c1d7df49be697a93f8f2a0" {
		// 	strat.CancelLiquidityCommitment()
		// }

	}
}

func (a *Agent) GetStrategies() []strats.Strategy {
	s := []strats.Strategy{}
	s = append(s, maps.Values(a.strategies)...)
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

func (a *Agent) GetBalances() []*AgentBalance {

	b := []*AgentBalance{}

	// We should really get these programatically instead of hardcoded...
	assets := map[string]string{
		"bf1e88d19db4b3ca0d1d5bdb73718a01686b18cf731ca26adedf3c8b83802bba": "USDT",
		"d1984e3d365faa05bcafbe41f50f90e3663ee7c0da22bb1e24b164e9532691b2": "VEGA",
	}

	type void struct{}
	seenAssets := map[string]void{}

	// Get settlement asset balances
	for _, strat := range a.strategies {
		settlementAsset := strat.GetMarketSettlementAsset()
		if _, ok := assets[settlementAsset]; !ok {
			log.Printf("error: no asset found with asset id: %s", settlementAsset)
			continue
		}
		if _, ok := seenAssets[settlementAsset]; ok {
			continue
		}

		seenAssets[settlementAsset] = void{}

		balance := strat.GetPubkeyBalance(settlementAsset)
		// balance := strat.GetAgentPubKeyBalance()

		b = append(b, &AgentBalance{
			Venue:     "Vega",
			AssetName: assets[settlementAsset], // We only have USDT markets for now
			AssetId:   settlementAsset,
			Balance:   balance,
		})
	}

	// Get a strategy
	strat := func() strats.Strategy {
		for _, strat := range a.strategies {
			return strat
		}
		return nil
	}()
	if strat == nil {
		log.Printf("Could not get VEGA balance: nil strategy returned from map.")
		return b
	}

	// Get VEGA token balance
	balance := strat.GetPubkeyBalance("d1984e3d365faa05bcafbe41f50f90e3663ee7c0da22bb1e24b164e9532691b2")
	b = append(b, &AgentBalance{
		Venue:     "Vega",
		AssetName: "VEGA",
		AssetId:   "d1984e3d365faa05bcafbe41f50f90e3663ee7c0da22bb1e24b164e9532691b2",
		Balance:   balance,
	})

	return b
}
