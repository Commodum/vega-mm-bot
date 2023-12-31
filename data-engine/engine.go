package data

import (
	"vega-mm/stores"
	strats "vega-mm/strategies"
)

type pubkey string
type vegaMarketId string
type binanceTicker string

type DataEngine struct {
	vegaCoreClient *VegaCoreClient
	vegaDataClient *VegaDataClient
	binanceClient  *BinanceClient

	// vegaStores    map[string][]*stores.VegaStore
	// binanceStores map[string][]*stores.BinanceStore

	vegaStores    map[vegaMarketId]map[pubkey]*stores.VegaStore
	binanceStores map[binanceTicker][]*stores.BinanceStore
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
		vegaStores:    map[vegaMarketId]map[pubkey]*stores.VegaStore{},
		binanceStores: map[binanceTicker][]*stores.BinanceStore{},
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
	binanceClient := NewBinanceClient(binanceWsAddr, d.binanceStores)
	vegaDataClient := NewVegaDataClient(vegaDataGrpcAddrs, d.vegaStores)

	return d
}

func (d *DataEngine) RegisterStrategies(strategies []strats.Strategy) *DataEngine {

	for _, strat := range strategies {
		agentPubKey := pubkey(strat.GetAgentPubKey())
		stratMarketId := vegaMarketId(strat.GetVegaMarketId())
		ticker := binanceTicker(strat.GetBinanceMarketTicker())

		pubkeyMap, ok := d.vegaStores[stratMarketId]
		if !ok {
			d.vegaStores[stratMarketId] = map[pubkey]*stores.VegaStore{}
			pubkeyMap = d.vegaStores[stratMarketId]
		}

		pubkeyMap[agentPubKey] = strat.GetVegaStore()
		d.binanceStores[ticker] = append(d.binanceStores[ticker], strat.GetBinanceStore())
	}

	return d
}

func (d *DataEngine) Start() {

}
