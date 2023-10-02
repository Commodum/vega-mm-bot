package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	// "time"

	wallet "github.com/jeremyletang/vega-go-sdk/wallet"
)

// Note: Below values configured for Fairground
const (
	defaultAdminPort           = 8080
	defaultVegaGrpcAddr        = "api.n00.testnet.vega.rocks:3007"
	defaultVegaGrpcAddresses   = "api.n00.testnet.vega.rocks:3007,api.n06.testnet.vega.rocks:3007,api.n07.testnet.vega.rocks:3007,api.n08.testnet.vega.rocks:3007,api.n09.testnet.vega.rocks:3007"
	defaultBinanceWsAddr       = "wss://stream.binance.com:443/ws"
	defaultWalletServiceAddr   = "http://127.0.0.1:1788" // Note: Fairground wallet running on port 1788
	defaultWalletPubkey        = ""
	defaultVegaMarkets         = "2fb61fb4a0ab63c7644d523efbd61b5236d90c7877b5b749c98f22988bac61ee"
	defaultBinanceMarkets      = "BTCUSDT,ETHUSDT,LINKUSDT"
	defaultLpMarket            = "2fb61fb4a0ab63c7644d523efbd61b5236d90c7877b5b749c98f22988bac61ee"
	defaultLpCommitmentSizeUSD = "5000"
)

var (
	adminPort           uint
	vegaGrpcAddr        string
	vegaGrpcAddresses   string
	binanceWsAddr       string
	walletServiceAddr   string
	walletToken         string
	walletPubkey        string
	vegaMarkets         string
	binanceMarkets      string
	lpMarket            string
	lpCommitmentSizeUSD string
)

func init() {
	fmt.Println("Initializing..")
	flag.UintVar(&adminPort, "admin-port", defaultAdminPort, "The port for the Admin API")
	flag.StringVar(&vegaGrpcAddr, "vega-grpc-addr", defaultVegaGrpcAddr, "A vega grpc server")
	flag.StringVar(&vegaGrpcAddresses, "vega-grpc-addresses", defaultVegaGrpcAddresses, "Vega grpc servers")
	flag.StringVar(&binanceWsAddr, "binance-ws-addr", defaultBinanceWsAddr, "A Binance websocket url")
	flag.StringVar(&walletServiceAddr, "wallet-service-addr", defaultWalletServiceAddr, "A vega wallet service address")
	flag.StringVar(&walletToken, "wallet-token", "", "a vega wallet token (for info see vega wallet token-api -h)")
	flag.StringVar(&walletPubkey, "wallet-pubkey", defaultWalletPubkey, "a vega public key")
	flag.StringVar(&vegaMarkets, "vega-markets", defaultVegaMarkets, "a comma separated list of market IDs")
	flag.StringVar(&binanceMarkets, "binance-markets", defaultBinanceMarkets, "a comma separated list of Binance markets")
	flag.StringVar(&lpMarket, "lp-market", defaultLpMarket, "The Vega market to submit an LP commitment to")
	flag.StringVar(&lpCommitmentSizeUSD, "lp-commitment-size", defaultLpCommitmentSizeUSD, "The size of the LP commitment in USD")
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
	// _, err := wallet.NewClient(defaultWalletServiceAddr, config.WalletToken)
	if err != nil {
		log.Fatalf("Could not connect to wallet: %v", err)
	}

	store := newDataStore()
	dataClient := newDataClient(config, store)

	var wg sync.WaitGroup
	wg.Add(2)

	go dataClient.runVegaClientReconnectHandler()
	go dataClient.streamBinanceData(&wg)
	go dataClient.streamVegaData(&wg)

	wg.Wait()

	// SetLiquidityCommitment(walletClient, dataClient)

	// time.Sleep(1 * time.Second)

	apiCh := make(chan *MetricsState)

	go RunStrategy(walletClient, dataClient, apiCh)

	go StartMetricsApi(apiCh)

	gracefulStop := make(chan os.Signal, 1)
	signal.Notify(gracefulStop, syscall.SIGTERM, syscall.SIGINT)
	<-gracefulStop

	// Should we flatten the inventory at shutdown?

	log.Print("Terminating due to user input.")

}
