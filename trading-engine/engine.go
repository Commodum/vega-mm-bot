package trading

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"
	"vega-mm/metrics"
	"vega-mm/pow"
	strats "vega-mm/strategies"
	"vega-mm/wallets"

	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"
	"github.com/shopspring/decimal"
	"golang.org/x/exp/maps"
)

// The trading engine will be responsible for monitoring agents.
// Data points that should be monitored include account balances, exposure,
// orderbook volume, trade volume, PnLs etc.
//
// The Trading Engine should monitor market level data across all trading
// venues that we are integrated with, it should aggregate order flow data
// across venues for analysis.
//
// The trading engine is responsible for initiation balance transfers between
// keys and periodically collecting allocated liquidity rewards and incentives.
type TradingEngine struct {
	mu sync.RWMutex

	embeddedVegaWallet *wallets.EmbeddedVegaWallet

	agents map[string]*Agent // map[pubkey]Agent

	hedger *Hedger

	metricsCh chan *metrics.MetricsEvent
}

func NewEngine() *TradingEngine {
	te := &TradingEngine{
		mu:     sync.RWMutex{},
		agents: map[string]*Agent{},
	}

	te.hedger = NewHedger(te)

	return te
}

func (t *TradingEngine) Init(metricsCh chan *metrics.MetricsEvent) *TradingEngine {
	// Determine how many agents are required based on the strategies
	// (Should we store the keypair index in each strategy?)

	homePath := os.Getenv("HOME")
	// mnemonic, err := os.ReadFile(fmt.Sprintf("%v/.config/vega-mm-fairground/embedded-wallet/words.txt", homePath))
	mnemonic, err := os.ReadFile(fmt.Sprintf("%v/.config/vega-mm/embedded-wallet/words.txt", homePath))
	if err != nil {
		log.Fatalf("Failed to read words from file: %v", err)
	}

	t.embeddedVegaWallet = wallets.NewWallet(string(mnemonic))
	t.metricsCh = metricsCh

	// kp2 := t.embeddedVegaWallet.GetKeyPair(2)
	// kp3 := t.embeddedVegaWallet.GetKeyPair(3)
	// kp4 := t.embeddedVegaWallet.GetKeyPair(4)

	// os.Exit(0)

	return t
}

func (t *TradingEngine) Start() {

	t.StartMetricsLoop()

	// Start the hedger.
	// t.hedger.Start(t.metricsCh)

	// To start trading, for each agent we need to:
	//	- Start the signer.
	//	- Load decimals.
	// 	- Update Liquidity Commitments.
	//	- Run strategies.

	for _, agent := range t.agents {
		agent.signer.Start()
		agent.LoadVegaDecimals()
		agent.UpdateLiquidityCommitments()
		agent.RunStrategies(t.metricsCh)
	}
}

func (t *TradingEngine) LoadStrategies(strategies []strats.Strategy, txBroadcastCh chan *commandspb.Transaction) {
	for _, strategy := range strategies {
		t.AddStrategy(strategy, txBroadcastCh)
	}

}

func (t *TradingEngine) AddStrategy(strat strats.Strategy, txBroadcastCh chan *commandspb.Transaction) {
	// Adding the strategy involes:
	//	- Generating the keypair for the agent index if not present
	//	- Instantiating the Agent if not already present
	//  - Registering the strategy with the agent

	keyPair := t.embeddedVegaWallet.GetKeyPair(strat.GetAgentKeyPairIndex())
	pubkey := keyPair.PubKey()

	agent, ok := t.agents[pubkey]
	if !ok {
		// Create new agent
		agent = NewAgent(keyPair, txBroadcastCh)
		t.agents[pubkey] = agent
	}

	agent.RegisterStrategy(strat)

}

func (t *TradingEngine) GetStrategies() []strats.Strategy {
	s := []strats.Strategy{}
	for _, agent := range maps.Values(t.agents) {
		s = append(s, agent.GetStrategies()...)
	}
	return s
}

func (t *TradingEngine) GetPowStores() map[string]*pow.PowStore {
	m := map[string]*pow.PowStore{}

	for pubkey, agent := range t.agents {
		m[pubkey] = agent.GetPowStore()
	}

	return m
}

func (t *TradingEngine) GetNumStratsPerAgent() map[string]int {
	m := map[string]int{}

	for pubkey, agent := range t.agents {
		m[pubkey] = len(agent.strategies)
	}

	return m
}

func (t *TradingEngine) GetNumAgents() int {
	return len(t.agents)
}

func (t *TradingEngine) ComputeMetrics() []*metrics.MetricsEvent {

	metricsEvents := []*metrics.MetricsEvent{}

	// Trading engine metrics will be total balances.
	// Agent metrics will be individual USDT and VEGA balances.
	globalBalances := map[string]decimal.Decimal{}

	for pubkey, agent := range t.agents {
		agent.mu.RLock()
		balances := agent.data.Balances
		agent.mu.RUnlock()

		agentBalances := map[string]decimal.Decimal{} //map[assetName]balance

		for _, bal := range balances {
			if _, ok := globalBalances[bal.AssetName]; !ok {
				globalBalances[bal.AssetName] = decimal.Zero
			}
			if _, ok := agentBalances[bal.AssetName]; !ok {
				agentBalances[bal.AssetName] = decimal.Zero
			}

			globalBalances[bal.AssetName] = globalBalances[bal.AssetName].Add(bal.Balance)
			agentBalances[bal.AssetName] = agentBalances[bal.AssetName].Add(bal.Balance)
		}

		metricsEvents = append(metricsEvents, &metrics.MetricsEvent{
			Type: metrics.MetricsEventType_Agent,
			Data: &metrics.AgentMetricsData{
				Pubkey:      pubkey,
				PubkeyIndex: int(agent.GetIndex()),
				VEGABalance: agentBalances["VEGA"],
				USDTBalance: agentBalances["USDT"],
			},
		})

	}

	metricsEvents = append(metricsEvents, &metrics.MetricsEvent{
		Type: metrics.MetricsEventType_TradingEngine,
		Data: &metrics.TradingEngineMetricsData{
			VEGABalance: globalBalances["VEGA"],
			USDTBalance: globalBalances["USDT"],
		},
	})

	return metricsEvents
}

// Starts monitoring agents by periodically recording data points like
// balances, exposure, orderbook volume, trade volume, PnLs etc.
// func (t *TradingEngine) StartAgentMonitoringLoop() {
func (t *TradingEngine) StartMetricsLoop() {
	go func() {
		for range time.NewTicker(time.Second * 3).C {

			// Update Agent data
			for _, agent := range t.agents {
				agent.mu.Lock()
				agent.data.Positions = agent.GetPositions()
				agent.data.Balances = agent.GetBalances()
				agent.mu.Unlock()
			}

			metricsEvents := t.ComputeMetrics()

			for _, evt := range metricsEvents {
				t.metricsCh <- evt
			}
		}
	}()
}
