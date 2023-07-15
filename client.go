package main

import (
	"fmt"
	"log"
	"encoding/json"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/shopspring/decimal"
)

type VegaClient struct {
	grpcAddr 	string
	vegaMarket 	string
}

type BinanceClient struct {
	wsAddr 			string
	binanceMarket 	string
}

type DataClient struct {
	b *BinanceClient
	v *VegaClient
}

func newDataClient(config *Config) *DataClient {
	return &DataClient{
		b: &BinanceClient{
			wsAddr: config.BinanceWsAddr,
			binanceMarket: config.BinanceMarket,
		},
		v: &VegaClient{
			grpcAddr: config.VegaGrpcAddr,
			vegaMarket: config.VegaMarket,
		}
	}
}


func (d *DataClient) streamBinanceData() {

	conn, _, err := websocket.DefaultDialer.Dial(d.b.wsAddr);
	if err != nil {
		log.Fatal("Dial Error: ", err)
	}
	defer conn.Close()

	req := struct{
		Id 		uint
		Method 	string
		Params 	string
	}{
		Id: 1,
		Method: "SUBSCRIBE",
		Params: []string
	}



}

func (d *DataClient) streamVegaData() {

}