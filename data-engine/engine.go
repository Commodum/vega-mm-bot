package data

import (
	"vega-mm/stores"
	strats "vega-mm/strategies"
)

type DataEngine struct {
	vegaCoreClient *VegaCoreClient
	vegaDataClient *VegaDataClient
	binanceClient  *BinanceClient

	vegaStores    []*stores.VegaStore
	binanceStores []*stores.BinanceStore

	// vegaStores    map[vegaMarketId]map[pubkey]*stores.VegaStore
	// binanceStores map[binanceTicker][]*stores.BinanceStore
}

// Need to decide how data should be collected. Should we have separate streams for each agent,
// market, or stratgey etc. Maybe one big stream with filters to get all the data and then we
// break the incoming data down by partyId, marketId, strategy etc.

// Potentially we want duplicate streams and then we can use the first response for each height
// and filter out the rest. Worth doing some tests to see which datanodes send responses first
// and determine if it is generally best to connect to the lowest latency datanode or to
// connect to multiple nodes and deduplicate responses.

func NewDataEngine() *DataEngine {
	return &DataEngine{
		vegaStores:    []*stores.VegaStore{},
		binanceStores: []*stores.BinanceStore{},
		// vegaStores:    map[vegaMarketId]map[pubkey]*stores.VegaStore{},
		// binanceStores: map[binanceTicker][]*stores.BinanceStore{},
	}
}

func (d *DataEngine) Init(binanceWsAddr string, vegaCoreGrpcAddrs []string, vegaDataGrpcAddrs []string) *DataEngine {

	// Init data engine.
	//	- Pass API endpoints from config.
	//	- Registers strategies with the Data Engine.
	//	- Stores pointers to data store for each strategy.
	//	- Opens API conns and streams.
	//	- Filters streams and directs data to corresponding strategy data stores.
	//	- Begins collecting spam statistics for use by the proof of work worker.

	// Instantiate clients
	binanceClient := NewBinanceClient(binanceWsAddr).Init(d.binanceStores)
	vegaDataClient := NewVegaDataClient(vegaDataGrpcAddrs).Init(d.vegaStores)
	vegCoreClient := NewVegaCoreClient(vegaCoreGrpcAddrs)

	return d
}

func (d *DataEngine) RegisterStrategies(strategies []strats.Strategy) *DataEngine {

	for _, strat := range strategies {
		d.vegaStores = append(d.vegaStores, strat.GetVegaStore())
		d.binanceStores = append(d.binanceStores, strat.GetBinanceStore())
	}

	return d
}

func (d *DataEngine) Start() {

}
