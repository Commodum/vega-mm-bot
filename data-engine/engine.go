package data

import "vega-mm/clients"

type DataEngine struct {
	vegaCoreClient *clients.VegaCoreClient
	vegaDataClient *clients.VegaDataClient
	binanceClient  *clients.BinanceClient

	strategies []

}