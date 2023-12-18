package trading

import "github.com/shopspring/decimal"

type Agent struct {
	// An agent will control one wallet and can run multiple strategies
	// Note: For the time being an agent will only allow one strategy per market.
	pubkey     string
	apiToken   string
	balance    decimal.Decimal
	strategies map[string]*strategy
	// walletClient *wallet.Client
	signer    *signer
	config    *Config
	metricsCh chan *MetricsState
}
