package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	wallet "github.com/jeremyletang/vega-go-sdk/wallet"
)

const (
	defaultAdminPort         = 8080
	defaultVegaGrpcAddr      = "datanode.vega.pathrocknetwork.org:3007" // "vega-mainnet-data-grpc.commodum.io:443" // "vega-data.nodes.guru:3007" "vega-data.bharvest.io:3007" "datanode.vega.pathrocknetwork.org:3007"
	defaultBinanceWsAddr     = "wss://stream.binance.com:443/ws"
	defaultWalletServiceAddr = "http://127.0.0.1:1789"
	defaultWalletPubkey      = ""
	defaultVegaMarket        = "5b05109662e7434fea498c4a1c91d3179b80e9b8950d6106cec60e1f342fc604"
	defaultBinanceMarket     = "BTCUSDT"
)

var (
	adminPort         uint
	vegaGrpcAddr      string
	binanceWsAddr     string
	walletServiceAddr string
	walletToken       string
	walletPubkey      string
	vegaMarket        string
	binanceMarket     string
)

func init() {
	fmt.Println("Initializing..")
	flag.UintVar(&adminPort, "admin-port", defaultAdminPort, "The port for the Admin API")
	flag.StringVar(&vegaGrpcAddr, "vega-grpc-addr", defaultVegaGrpcAddr, "A vega grpc server")
	flag.StringVar(&binanceWsAddr, "binance-ws-addr", defaultBinanceWsAddr, "A Binance websocket url")
	flag.StringVar(&walletServiceAddr, "wallet-service-addr", defaultWalletServiceAddr, "A vega wallet service address")
	flag.StringVar(&walletToken, "wallet-token", "", "a vega wallet token (for info see vega wallet token-api -h)")
	flag.StringVar(&walletPubkey, "wallet-pubkey", defaultWalletPubkey, "a vega public key")
	flag.StringVar(&vegaMarket, "vega-market", defaultVegaMarket, "a vega market id")
	flag.StringVar(&binanceMarket, "binance-market", defaultBinanceMarket, "a binance market symbol")

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
	walletClient, err := wallet.NewClient(defaultWalletServiceAddr, config.WalletToken)
	if err != nil {
		log.Fatalf("Could not connect to wallet: %v", err)
	}

	store := newDataStore(binanceMarket)
	dataClient := newDataClient(config, store)

	var wg sync.WaitGroup
	wg.Add(2)

	go dataClient.streamBinanceData(&wg)
	go dataClient.streamVegaData(&wg)

	wg.Wait()

	go RunStrategy(walletClient, dataClient)

	gracefulStop := make(chan os.Signal, 1)
	signal.Notify(gracefulStop, syscall.SIGTERM, syscall.SIGINT)
	<-gracefulStop

	// Should we flatten the inventory at shutdown?

	log.Print("Terminating due to user input.")

}
