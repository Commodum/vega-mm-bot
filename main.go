package main

import (
	"fmt"
	"flag"

	wallet "github.com/jeremyletang/vega-go-sdk/wallet"
)

const (
	defaultAdminPort			= 8080
	defaultVegaGrpcAddr 		= "tls://vega-mainnet-data-grpc.commodum.io:443" // "vega-data.nodes.guru:3007" "vega-data.bharvest.io:3007" "datanode.vega.pathrocknetwork.org:3007"
	defaultBinanceWsAddr 		= "wss://ws-api.binance.com:443/ws-api/v3"
	defaultWalletServiceAddr 	= "http://127.0.0.1:1789"
)

var (
	adminPort 			int
	vegaGrpcAddr		string
	binanceWsAddr		string
	walletServiceAddr 	string
	walletToken			string
	walletPubkey		string
	vegaMarket			string
	binanceMarket		string
)

func init() {
	fmt.Println("Initializing..")
	flag.Uintvar(&adminPort, "admin-port", defaultAdminPort, "The port for the Admin API")
	flag.StringVar(&vegaGrpcAddr, "vega-grpc-addr", defaultVegaGrpcAddr, "A vega grpc server")
	flag.StringVar(&binanceWsAddr, "binance-ws-addr", defaultBinanceWsAddr, "A Binance websocket url")
	flag.StringVar(&walletServiceAddr, "wallet-service-addr", defaultWalletServiceAddr, "A vega wallet service address")
	flag.StringVar(&walletToken, "wallet-token", "", "a vega wallet token (for info see vega wallet token-api -h)")
	flag.StringVar(&walletPubkey, "wallet-pubkey", "", "a vega public key")
	flag.StringVar(&vegaMarket, "vega-market", "", "a vega market id")
	flag.StringVar(&binanceMarket, "binance-market", "", "a binance market symbol")


}

func main() {

	// Things we need to do:
	// 	a). Connect to wallet
	//	b). Init data store
	//	c). Subscribe to external prices
	//	d). Get current bot state from Vega
	//	e). Subscribe to vega data
	//	f). Run strategy

	// Get config
	config := parseFlags()

	// a).
	w, err := wallet.NewClient(defaultWalletServiceAddr, token)
	if err != nil {
		log.Fatalf("Could not connect to wallet: %v", err);
	}

	// b).
	store := newDataStore(binanceMarket)
	client := newDataClient(config)

	// c).
	go client.streamBinanceData(config, dataStore)

	go client.streamVegaData(config, dataStore)

}