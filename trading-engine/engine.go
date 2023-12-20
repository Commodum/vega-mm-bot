package trading

// The trading engine will be responsible for monitoring agents.
// Data points that should be monitored include account balances, exposure,
// orderbook volume, trade volume, PnLs etc. The engine should be able
// The Trading Engine should monitor market level data across all trading
// venues that we are integrated with, it should aggregate order flow data
// across venues for analysis.
type TradingEngine struct {
}
