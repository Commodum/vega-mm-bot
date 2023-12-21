package data

import "vega-mm/clients"

type DataEngine struct {
	vegaCoreClient *clients.VegaCoreClient
	vegaDataClient *clients.VegaDataClient
	binanceClient  *clients.BinanceClient

	vegaStores    []*VegaStore
	binanceStores []*BinanceStore
}

// Need to decide how data should be collected. Should we have separate streams for each agent,
// market, or stratgey etc. Maybe one big stream with filters to get al the data and then we
// break the incoming data down by partyId, marketId, strategy etc.
