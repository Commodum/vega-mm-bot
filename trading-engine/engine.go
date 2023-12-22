package trading

import "sync"

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

	agents map[string]Agent // map[pubkey]Agent

}

func (t *TradingEngine) Init() {

}

func (t *TradingEngine) RegisterAgent() {

}

func (t *TradingEngine) LoadStrategies() {

}

func (t *TradingEngine) AddStrategy() {

}
