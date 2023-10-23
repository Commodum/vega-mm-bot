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
	defaultVegaMarkets         = "69abf5c456c20f4d189cea79a11dfd6b0958ead58ab34bd66f73eea48aee600c"
	defaultBinanceMarkets      = "BTCUSDT,ETHUSDT,LINKUSDT"
	defaultLpMarket            = "69abf5c456c20f4d189cea79a11dfd6b0958ead58ab34bd66f73eea48aee600c"
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

	agent := NewAgent(walletClient, config)

	// Load strategyOpts from file.
	// Note: Don't do this, just use flags...
	// stratOpts := loadJsonConfig()

	strategyOpts := &StrategyOpts{
		marketId:                "69abf5c456c20f4d189cea79a11dfd6b0958ead58ab34bd66f73eea48aee600c",
		lpCommitmentSizeUSD:     5000,
		maxProbabilityOfTrading: 0.8,
		orderSpacing:            0.001,
		orderSizeBase:           2.0,
		targetVolCoefficient:    1.25,
		numOrdersPerSide:        8,
	}

	strategy := NewStrategy(strategyOpts, config)

	agent.RegisterStrategy(strategy)

	var wg sync.WaitGroup
	wg.Add(1)

	go agent.VegaClient().RunVegaClientReconnectHandler()
	go agent.VegaClient().StreamVegaData(&wg)

	wg.Wait()

	agent.LoadDecimals()

	agent.UpdateLiquidityCommitment(strategy)

	metricsCh := make(chan *MetricsState)

	go agent.RunStrategy(strategy, metricsCh)

	go StartMetricsApi(metricsCh)

	gracefulStop := make(chan os.Signal, 1)
	signal.Notify(gracefulStop, syscall.SIGTERM, syscall.SIGINT)
	<-gracefulStop

	// Should we flatten the inventory at shutdown?

	log.Print("Terminating due to user input.")

}
