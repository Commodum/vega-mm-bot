package trading

import (
	"fmt"
	"log"
	"os"
	"sync"
	"vega-mm/metrics"
	strats "vega-mm/strategies"
	"vega-mm/wallets"

	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"
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

	metricsCh chan *metrics.MetricsEvent
}

func NewEngine() *TradingEngine {
	return &TradingEngine{
		mu:     sync.RWMutex{},
		agents: map[string]*Agent{},
	}
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

	return t
}

func (t *TradingEngine) RegisterAgent() {

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
