package metrics

import (
	// "encoding/json"
	// "fmt"
	"log"
	"net/http"

	vegapb "code.vegaprotocol.io/vega/protos/vega"
	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/shopspring/decimal"
	// vegapb "vega-mm/protos/vega"
)

type MetricsEventType int

// Maybe we should add a Venue metrics event as well so that
// we can keep track of metrics specific to each trading venue.
const (
	MetricsEventType_Unspecified MetricsEventType = iota
	MetricsEventType_Strategy
	MetricsEventType_Agent
	MetricsEventType_DataEngine
	MetricsEventType_TradingEngine
	MetricsEventType_Pow
)

func (m MetricsEventType) String() string {
	switch m {
	case 0:
		return "MetricsEventType_Unspecified"
	case 1:
		return "MetricsEventType_Strategy"
	case 2:
		return "MetricsEventType_Agent"
	case 3:
		return "MetricsEventType_DataEngine"
	case 4:
		return "MetricsEventType_TradingEngine"
	case 5:
		return "MetricsEventType_Pow"
	default:
		return "MetricsEventType_Unspecified"
	}
}

type MetricsEventData interface {
	isMetricsData()
}

func (m *StrategyMetricsData) isMetricsData()      {}
func (m *AgentMetricsData) isMetricsData()         {}
func (m *DataEngineMetricsData) isMetricsData()    {}
func (m *TradingEngineMetricsData) isMetricsData() {}
func (m *PowMetricsData) isMetricsData()           {}

type StrategyMetricsData struct {
	MarketId                  string
	BinanceTicker             string
	AgentPubkey               string
	Position                  *vegapb.Position
	SignedExposure            decimal.Decimal
	VegaBestBid               decimal.Decimal
	OurBestBid                decimal.Decimal
	VegaBestAsk               decimal.Decimal
	OurBestAsk                decimal.Decimal
	LiveOrdersCount           int
	MarketDataUpdateCount     int
	TimeSinceMarketDataUpdate int
}

type AgentMetricsData struct {
	Pubkey      string
	PubkeyIndex int
	VEGABalance decimal.Decimal
	USDTBalance decimal.Decimal
}

type DataEngineMetricsData struct{}

type TradingEngineMetricsData struct {
	VEGABalance decimal.Decimal
	USDTBalance decimal.Decimal
}

type PowMetricsData struct{}

type StrategyMetrics struct {
	SignedExposure            *prom.GaugeVec
	VegaBestBid               *prom.GaugeVec
	OurBestBid                *prom.GaugeVec
	VegaBestAsk               *prom.GaugeVec
	OurBestAsk                *prom.GaugeVec
	LiveOrderCount            *prom.GaugeVec
	CumulativeOrderCount      *prom.CounterVec
	MarketDataUpdateCount     *prom.GaugeVec
	TimeSinceMarketDataUpdate *prom.GaugeVec
}

type AgentMetrics struct {
	VEGABalance *prom.GaugeVec
	USDTBalance *prom.GaugeVec
}

type TradingEngineMetrics struct {
	VEGABalance prom.Gauge
	USDTBalance prom.Gauge
}

type MetricsEvent struct {
	Type MetricsEventType
	Data MetricsEventData
}

func (m *MetricsEvent) GetType() MetricsEventType {
	return m.Type
}

func (m *MetricsEvent) GetData() MetricsEventData {
	return m.Data
}

func (m *MetricsServer) HandleEvent(evt *MetricsEvent) {

	evtType := evt.GetType()
	data := evt.GetData()

	switch evtType {
	case MetricsEventType_Strategy:
		m.handleStrategyMetricsData(data.(*StrategyMetricsData))
	case MetricsEventType_Agent:
		m.handleAgentMetricsData(data.(*AgentMetricsData))
	case MetricsEventType_DataEngine:
		data = data.(*DataEngineMetricsData)
		log.Printf("No handling implemented for event of type: %v", evtType.String())
	case MetricsEventType_TradingEngine:
		m.handleTradingEngineMetricsData(data.(*TradingEngineMetricsData))
	case MetricsEventType_Pow:
		data = data.(*PowMetricsData)
		log.Printf("No handling implemented for event of type: %v", evtType.String())
	default:
		log.Printf("Unknown Metrics Event Type received: %v", evtType.String())
	}
}

type MetricsServer struct {
	Port     string
	inCh     chan *MetricsEvent
	registry *prom.Registry

	strategyMetrics      *StrategyMetrics
	agentMetrics         *AgentMetrics
	tradingEngineMetrics *TradingEngineMetrics
}

func NewMetricsServer(port string) *MetricsServer {
	return &MetricsServer{
		Port:     port,
		inCh:     make(chan *MetricsEvent),
		registry: prom.NewRegistry(),
	}
}

func (m *MetricsServer) Init() chan *MetricsEvent {
	m.SetStrategyMetrics()
	m.SetAgentMetrics()
	m.SetTradingEngineMetrics()

	m.RegisterStrategyMetrics()
	m.RegisterAgentMetrics()
	m.RegisterTradingEngineMetrics()

	return m.inCh
}

func (m *MetricsServer) Start() {

	go func() {
		for evt := range m.inCh {
			m.HandleEvent(evt)
		}
	}()

	http.Handle("/metrics", promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{Registry: m.registry}))
	log.Fatal(http.ListenAndServe(m.Port, nil))
}

func (m *MetricsServer) SetStrategyMetrics() {
	m.strategyMetrics = &StrategyMetrics{
		SignedExposure: prom.NewGaugeVec(prom.GaugeOpts{
			Namespace: "vega_mm",
			Name:      "signed_exposure",
			Help:      "Our current signed exposure in the base asset of the market. +ve means Long, -ve means Short",
		}, []string{
			"ticker", "pubkey",
		}),
		VegaBestBid: prom.NewGaugeVec(prom.GaugeOpts{
			Namespace: "vega_mm",
			Name:      "vega_best_bid",
			Help:      "The highest bid in the book on Vega for this market",
		}, []string{
			"ticker", "pubkey",
		}),
		OurBestBid: prom.NewGaugeVec(prom.GaugeOpts{
			Namespace: "vega_mm",
			Name:      "our_best_bid",
			Help:      "Our highest bid in the book on Vega for this market",
		}, []string{
			"ticker", "pubkey",
		}),
		VegaBestAsk: prom.NewGaugeVec(prom.GaugeOpts{
			Namespace: "vega_mm",
			Name:      "vega_best_ask",
			Help:      "The lowest ask in the order book",
		}, []string{
			"ticker", "pubkey",
		}),
		OurBestAsk: prom.NewGaugeVec(prom.GaugeOpts{
			Namespace: "vega_mm",
			Name:      "our_best_ask",
			Help:      "Our lowest ask in the order book",
		}, []string{
			"ticker", "pubkey",
		}),
		LiveOrderCount: prom.NewGaugeVec(prom.GaugeOpts{
			Namespace: "vega_mm",
			Name:      "live_order_count",
			Help:      "The number of live orders we have placed in the order book",
		}, []string{
			"ticker", "pubkey",
		}),
		CumulativeOrderCount: prom.NewCounterVec(prom.CounterOpts{
			Namespace: "vega_mm",
			Name:      "cumulative_order_count",
			Help:      "A monotonically increasing count of the number of orders successfully placed",
		}, []string{
			"ticker", "pubkey",
		}),
		MarketDataUpdateCount: prom.NewGaugeVec(prom.GaugeOpts{
			Namespace: "vega_mm",
			Name:      "market_data_update_count",
			Help:      "A monotonically increasing count of the number of times market data has been updated",
		}, []string{
			"ticker", "pubkey",
		}),
	}
}

func (m *MetricsServer) SetAgentMetrics() {
	m.agentMetrics = &AgentMetrics{
		VEGABalance: prom.NewGaugeVec(prom.GaugeOpts{
			Namespace: "vega_mm",
			Name:      "vega_balance",
			Help:      "The current balance of VEGA tokens for the agent.",
		}, []string{
			"pubkey",
		}),
		USDTBalance: prom.NewGaugeVec(prom.GaugeOpts{
			Namespace: "vega_mm",
			Name:      "usdt_balance",
			Help:      "The current balance of USDT for the agent.",
		}, []string{
			"pubkey",
		}),
	}
}

func (m *MetricsServer) SetTradingEngineMetrics() {
	m.tradingEngineMetrics = &TradingEngineMetrics{
		VEGABalance: prom.NewGauge(prom.GaugeOpts{
			Namespace: "vega_mm",
			Name:      "global_vega_balance",
			Help:      "The current global balance of VEGA tokens.",
		}),
		USDTBalance: prom.NewGauge(prom.GaugeOpts{
			Namespace: "vega_mm",
			Name:      "global_usdt_balance",
			Help:      "The current global balance of USDT.",
		}),
	}
}

func (m *MetricsServer) GetStrategyMetrics() *StrategyMetrics {
	return m.strategyMetrics
}

func (m *MetricsServer) GetAgentMetrics() *AgentMetrics {
	return m.agentMetrics
}

func (m *MetricsServer) GetTradingEngineMetrics() *TradingEngineMetrics {
	return m.tradingEngineMetrics
}

func (m *MetricsServer) RegisterStrategyMetrics() {
	m.registry.MustRegister(m.strategyMetrics.SignedExposure)
	m.registry.MustRegister(m.strategyMetrics.VegaBestBid)
	m.registry.MustRegister(m.strategyMetrics.OurBestBid)
	m.registry.MustRegister(m.strategyMetrics.VegaBestAsk)
	m.registry.MustRegister(m.strategyMetrics.OurBestAsk)
	m.registry.MustRegister(m.strategyMetrics.LiveOrderCount)
	m.registry.MustRegister(m.strategyMetrics.CumulativeOrderCount)
	m.registry.MustRegister(m.strategyMetrics.MarketDataUpdateCount)
}

func (m *MetricsServer) RegisterAgentMetrics() {
	m.registry.MustRegister(m.agentMetrics.VEGABalance)
	m.registry.MustRegister(m.agentMetrics.USDTBalance)
}

func (m *MetricsServer) RegisterTradingEngineMetrics() {
	m.registry.MustRegister(m.tradingEngineMetrics.VEGABalance)
	m.registry.MustRegister(m.tradingEngineMetrics.USDTBalance)
}

func (m *MetricsServer) handleStrategyMetricsData(data *StrategyMetricsData) {
	metrics := m.GetStrategyMetrics()
	metrics.SignedExposure.With(prom.Labels{"ticker": data.BinanceTicker, "pubkey": data.AgentPubkey}).Set(data.SignedExposure.InexactFloat64())
	metrics.VegaBestBid.With(prom.Labels{"ticker": data.BinanceTicker, "pubkey": data.AgentPubkey}).Set(data.VegaBestBid.InexactFloat64())
	metrics.OurBestBid.With(prom.Labels{"ticker": data.BinanceTicker, "pubkey": data.AgentPubkey}).Set(data.OurBestBid.InexactFloat64())
	metrics.VegaBestAsk.With(prom.Labels{"ticker": data.BinanceTicker, "pubkey": data.AgentPubkey}).Set(data.VegaBestAsk.InexactFloat64())
	metrics.OurBestAsk.With(prom.Labels{"ticker": data.BinanceTicker, "pubkey": data.AgentPubkey}).Set(data.OurBestAsk.InexactFloat64())
	metrics.LiveOrderCount.With(prom.Labels{"ticker": data.BinanceTicker, "pubkey": data.AgentPubkey}).Set(float64(data.LiveOrdersCount))
	metrics.CumulativeOrderCount.With(prom.Labels{"ticker": data.BinanceTicker, "pubkey": data.AgentPubkey}).Add(float64(data.LiveOrdersCount))
	metrics.MarketDataUpdateCount.With(prom.Labels{"ticker": data.BinanceTicker, "pubkey": data.AgentPubkey}).Set(float64(data.MarketDataUpdateCount))
}

func (m *MetricsServer) handleAgentMetricsData(data *AgentMetricsData) {
	metrics := m.GetAgentMetrics()
	metrics.VEGABalance.With(prom.Labels{"pubkey": data.Pubkey}).Set(data.VEGABalance.InexactFloat64())
	metrics.USDTBalance.With(prom.Labels{"pubkey": data.Pubkey}).Set(data.USDTBalance.InexactFloat64())
}

func (m *MetricsServer) handleTradingEngineMetricsData(data *TradingEngineMetricsData) {
	metrics := m.GetTradingEngineMetrics()
	metrics.VEGABalance.Set(data.VEGABalance.InexactFloat64())
	metrics.USDTBalance.Set(data.USDTBalance.InexactFloat64())
}
