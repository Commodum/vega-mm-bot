package main

import (
	"fmt"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"log"

	// wallet "github.com/jeremyletang/vega-go-sdk/wallet"
)

const (
	defaultAdminPort			= 8080
	defaultVegaGrpcAddr 		= "datanode.vega.pathrocknetwork.org:3007" // "vega-mainnet-data-grpc.commodum.io:443" // "vega-data.nodes.guru:3007" "vega-data.bharvest.io:3007" "datanode.vega.pathrocknetwork.org:3007"
	defaultBinanceWsAddr 		= "wss://stream.binance.com:443/ws"
	defaultWalletServiceAddr 	= "http://127.0.0.1:1789"
)

var (
	adminPort 			uint
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
	flag.UintVar(&adminPort, "admin-port", defaultAdminPort, "The port for the Admin API")
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
	// w, err := wallet.NewClient(defaultWalletServiceAddr, config.WalletToken)
	// if err != nil {
	// 	log.Fatalf("Could not connect to wallet: %v", err);
	// }

	store := newDataStore(binanceMarket)
	dataClient := newDataClient(config, store)

	go dataClient.streamBinanceData()
	go dataClient.streamVegaData()

	go RunStrategy(walletClient, dataClient)

	gracefulStop := make(chan os.Signal, 1)
	signal.Notify(gracefulStop, syscall.SIGTERM, syscall.SIGINT)
	<-gracefulStop

	log.Print("Terminating due to user input.")
	
}