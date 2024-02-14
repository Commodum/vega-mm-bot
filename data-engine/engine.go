package data

import (
	"sync"
	"vega-mm/pow"
	"vega-mm/stores"
	strats "vega-mm/strategies"

	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"
	"golang.org/x/exp/maps"
)

type DataEngine struct {
	vegaCoreClient *VegaCoreClient
	vegaDataClient *VegaDataClient
	binanceClient  *BinanceClient

	agentPubkeys  []string
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

func (d *DataEngine) Init(binanceWsAddr string, vegaCoreGrpcAddrs []string, vegaDataGrpcAddrs []string, txBroadcastCh chan *commandspb.Transaction, recentBlockCh chan *pow.RecentBlock) *DataEngine {

	// Init data engine.
	//	- Pass API endpoints from config.
	//	- Creates and initializes clients.

	d.binanceClient = NewBinanceClient(binanceWsAddr).Init(d.binanceStores)
	d.vegaDataClient = NewVegaDataClient(vegaDataGrpcAddrs).Init(d.vegaStores)
	d.vegaCoreClient = NewVegaCoreClient(vegaCoreGrpcAddrs).Init(d.agentPubkeys, txBroadcastCh, recentBlockCh)

	return d
}

func (d *DataEngine) RegisterStrategies(strategies []strats.Strategy) *DataEngine {

	agentPubkeys := map[string]struct{}{}

	for _, strat := range strategies {
		agentPubkeys[strat.GetAgentPubKey()] = struct{}{}
		d.vegaStores = append(d.vegaStores, strat.GetVegaStore())
		d.binanceStores = append(d.binanceStores, strat.GetBinanceStore())
	}

	d.agentPubkeys = maps.Keys(agentPubkeys)

	return d
}

func (d *DataEngine) Start(wg *sync.WaitGroup) {

	// Starts the data engine.
	//	- Opens API conns and streams.
	//	- Begins collecting spam statistics for use by the proof of work worker.

	// We need to wait until the streams are open before we continue.

	d.vegaCoreClient.Start()
	d.binanceClient.Start(wg)
	d.vegaDataClient.Start(wg)

}
