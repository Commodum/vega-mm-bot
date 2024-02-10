package trading

import (
	"time"
	"vega-mm/metrics"
)

// Mock DyDx Transaction
type DyDxTx struct{}

// Hedger is responsible for tracking exposure across relevant strategies and
// hedging it for all applicable strategies.
type Hedger struct {
	txOutCh chan *DyDxTx

	te *TradingEngine
}

func NewHedger(te *TradingEngine) *Hedger {
	return &Hedger{
		txOutCh: make(chan *DyDxTx),
		te:      te,
	}
}

// The hedger will need to aggregate all positions held by all
// agents, calculate the net exposure, and then hedge the net.

// Starts a loop that checks for unhedged exposure and hedges it.
func (h *Hedger) Start(metricsCh chan *metrics.MetricsEvent) {
	go func() {
		for range time.NewTicker(time.Second * 5).C {

			// Get all positions
			positions := []*Position{}
			for _, strat := range h.te.GetStrategies() {
				ticker := strat.GetBinanceMarketTicker()
				vol, ep := strat.GetEntryPriceAndVolume()
				pos := &Position{
					Venue:      "Vega", // For now we only trade on Vega...
					Ticker:     ticker,
					OpenVolume: vol,
					EntryPrice: ep,
				}
				positions = append(positions, pos)
			}

			// Calculate net exposure for each market

			// Build hedging txs

			// Send hedging txs
			h.txOutCh <- &DyDxTx{}

		}

	}()
}
