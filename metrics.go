package main

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

type MetricsState struct {
	MarketId                  string
	BinanceTicker             string
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

// Add metrics for data updates and streams
type PromMetrics struct {
	SignedExposure            prom.Gauge
	VegaBestBid               prom.Gauge
	OurBestBid                prom.Gauge
	VegaBestAsk               prom.Gauge
	OurBestAsk                prom.Gauge
	LiveOrderCount            prom.Gauge
	CumulativeOrderCount      prom.Counter
	MarketDataUpdateCount     prom.Gauge
	TimeSinceMarketDataUpdate prom.Gauge
}

type PromMetricVectors struct {
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

func StartMetricsApi(metricsCh chan *MetricsState) {

	reg := prom.NewRegistry()
	metrics := &PromMetrics{
		SignedExposure: prom.NewGauge(prom.GaugeOpts{
			Name: "signed_exposure",
			Help: "Our current signed exposure in the base asset of the market. +ve means Long, -ve means Short",
		}),
		VegaBestBid: prom.NewGauge(prom.GaugeOpts{
			Name: "vega_best_bid",
			Help: "The highest bid in the order book",
		}),
		OurBestBid: prom.NewGauge(prom.GaugeOpts{
			Name: "our_best_bid",
			Help: "Our highest bid in the order book",
		}),
		VegaBestAsk: prom.NewGauge(prom.GaugeOpts{
			Name: "vega_best_ask",
			Help: "The lowest ask in the order book",
		}),
		OurBestAsk: prom.NewGauge(prom.GaugeOpts{
			Name: "our_best_ask",
			Help: "Our lowest ask in the order book",
		}),
		LiveOrderCount: prom.NewGauge(prom.GaugeOpts{
			Name: "live_order_count",
			Help: "The number of live orders we have placed in the order book",
		}),
		CumulativeOrderCount: prom.NewCounter(prom.CounterOpts{
			Name: "cumulative_order_count",
			Help: "A monotonically increasing count of the number of orders successfully placed",
		}),
		MarketDataUpdateCount: prom.NewGauge(prom.GaugeOpts{
			Name: "market_data_update_count",
			Help: "A monotonically increasing count of the number of times market data has been updated",
		}),
	}

	_ = metrics // Keep compiler happy

	metricVecs := &PromMetricVectors{
		SignedExposure: prom.NewGaugeVec(prom.GaugeOpts{
			Namespace: "vega_mm",
			Name:      "signed_exposure_vector",
			Help:      "Our current signed exposure in the base asset of the market. +ve means Long, -ve means Short",
		}, []string{
			"marketId",
		}),
		VegaBestBid: prom.NewGaugeVec(prom.GaugeOpts{
			Namespace: "vega_mm",
			Name:      "vega_best_bid",
			Help:      "The highest bid in the order book",
		}, []string{
			"marketId",
		}),
		OurBestBid: prom.NewGaugeVec(prom.GaugeOpts{
			Namespace: "vega_mm",
			Name:      "our_best_bid",
			Help:      "Our highest bid in the order book",
		}, []string{
			"marketId",
		}),
		VegaBestAsk: prom.NewGaugeVec(prom.GaugeOpts{
			Namespace: "vega_mm",
			Name:      "vega_best_ask",
			Help:      "The lowest ask in the order book",
		}, []string{
			"marketId",
		}),
		OurBestAsk: prom.NewGaugeVec(prom.GaugeOpts{
			Namespace: "vega_mm",
			Name:      "our_best_ask",
			Help:      "Our lowest ask in the order book",
		}, []string{
			"marketId",
		}),
		LiveOrderCount: prom.NewGaugeVec(prom.GaugeOpts{
			Namespace: "vega_mm",
			Name:      "live_order_count",
			Help:      "The number of live orders we have placed in the order book",
		}, []string{
			"marketId",
		}),
		CumulativeOrderCount: prom.NewCounterVec(prom.CounterOpts{
			Namespace: "vega_mm",
			Name:      "cumulative_order_count",
			Help:      "A monotonically increasing count of the number of orders successfully placed",
		}, []string{
			"marketId",
		}),
		MarketDataUpdateCount: prom.NewGaugeVec(prom.GaugeOpts{
			Namespace: "vega_mm",
			Name:      "market_data_update_count",
			Help:      "A monotonically increasing count of the number of times market data has been updated",
		}, []string{
			"marketId",
		}),
	}

	reg.MustRegister(metricVecs.SignedExposure)
	reg.MustRegister(metricVecs.VegaBestBid)
	reg.MustRegister(metricVecs.OurBestBid)
	reg.MustRegister(metricVecs.VegaBestAsk)
	reg.MustRegister(metricVecs.OurBestAsk)
	reg.MustRegister(metricVecs.LiveOrderCount)
	reg.MustRegister(metricVecs.CumulativeOrderCount)
	reg.MustRegister(metricVecs.MarketDataUpdateCount)

	var state *MetricsState
	go func() {
		for {
			select {
			case state = <-metricsCh:

				metricVecs.SignedExposure.With(prom.Labels{"marketId": state.MarketId}).Set(state.SignedExposure.InexactFloat64())
				metricVecs.VegaBestBid.With(prom.Labels{"marketId": state.MarketId}).Set(state.VegaBestBid.InexactFloat64())
				metricVecs.OurBestBid.With(prom.Labels{"marketId": state.MarketId}).Set(state.OurBestBid.InexactFloat64())
				metricVecs.VegaBestAsk.With(prom.Labels{"marketId": state.MarketId}).Set(state.VegaBestAsk.InexactFloat64())
				metricVecs.OurBestAsk.With(prom.Labels{"marketId": state.MarketId}).Set(state.OurBestAsk.InexactFloat64())
				metricVecs.LiveOrderCount.With(prom.Labels{"marketId": state.MarketId}).Set(float64(state.LiveOrdersCount))
				metricVecs.CumulativeOrderCount.With(prom.Labels{"marketId": state.MarketId}).Add(float64(state.LiveOrdersCount))
				metricVecs.MarketDataUpdateCount.With(prom.Labels{"marketId": state.MarketId}).Set(float64(state.MarketDataUpdateCount))

				// metrics.SignedExposure.Set(state.SignedExposure.InexactFloat64())
				// metrics.VegaBestBid.Set(state.VegaBestBid.InexactFloat64())
				// metrics.OurBestBid.Set(state.OurBestBid.InexactFloat64())
				// metrics.VegaBestAsk.Set(state.VegaBestAsk.InexactFloat64())
				// metrics.OurBestAsk.Set(state.OurBestAsk.InexactFloat64())
				// metrics.LiveOrderCount.Set(float64(state.LiveOrdersCount))
				// metrics.CumulativeOrderCount.Add(float64(state.LiveOrdersCount))
				// metrics.MarketDataUpdateCount.Set(float64(state.MarketDataUpdateCount))
			}
		}
	}()

	// http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
	// 	out, _ := json.MarshalIndent(&metrics, "", "    ")
	// 	fmt.Fprintf(w, "%v", string(out))
	// })

	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
	log.Fatal(http.ListenAndServe(":8080", nil))

	// Fairground
	// log.Fatal(http.ListenAndServe(":8079", nil))
}
