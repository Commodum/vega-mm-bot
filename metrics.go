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
)

type ApiState struct {
	MarketId              string
	Position              *vegapb.Position
	SignedExposure        decimal.Decimal
	VegaBestBid           decimal.Decimal
	OurBestBid            decimal.Decimal
	VegaBestAsk           decimal.Decimal
	OurBestAsk            decimal.Decimal
	LiveOrdersCount       int
	MarketDataUpdateCount int
}

// Add metrics for data updates and streams
type PromMetrics struct {
	SignedExposure        prom.Gauge
	VegaBestBid           prom.Gauge
	OurBestBid            prom.Gauge
	VegaBestAsk           prom.Gauge
	OurBestAsk            prom.Gauge
	LiveOrderCount        prom.Gauge
	CumulativeOrderCount  prom.Counter
	MarketDataUpdateCount prom.Gauge
}

func StartApi(apiCh chan *ApiState) {

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

	reg.MustRegister(metrics.SignedExposure)
	reg.MustRegister(metrics.VegaBestBid)
	reg.MustRegister(metrics.OurBestBid)
	reg.MustRegister(metrics.VegaBestAsk)
	reg.MustRegister(metrics.OurBestAsk)
	reg.MustRegister(metrics.LiveOrderCount)
	reg.MustRegister(metrics.CumulativeOrderCount)
	reg.MustRegister(metrics.MarketDataUpdateCount)

	var state *ApiState
	go func() {
		for {
			select {
			case state = <-apiCh:
				metrics.SignedExposure.Set(state.SignedExposure.InexactFloat64())
				metrics.VegaBestBid.Set(state.VegaBestBid.InexactFloat64())
				metrics.OurBestBid.Set(state.OurBestBid.InexactFloat64())
				metrics.VegaBestAsk.Set(state.VegaBestAsk.InexactFloat64())
				metrics.OurBestAsk.Set(state.OurBestAsk.InexactFloat64())
				metrics.LiveOrderCount.Set(float64(state.LiveOrdersCount))
				metrics.CumulativeOrderCount.Add(float64(state.LiveOrdersCount))
				metrics.MarketDataUpdateCount.Set(float64(state.MarketDataUpdateCount))
			}
		}
	}()

	// http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
	// 	out, _ := json.MarshalIndent(&metrics, "", "    ")
	// 	fmt.Fprintf(w, "%v", string(out))
	// })

	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
	log.Fatal(http.ListenAndServe(":8080", nil))
}
