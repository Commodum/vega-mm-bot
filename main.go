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

const (
	defaultAdminPort           = 8080
	defaultVegaGrpcAddr        = "vega-data.nodes.guru:3007"                                                                                                                                                // "datanode.vega.pathrocknetwork.org:3007" // "vega-mainnet-data-grpc.commodum.io:443" // "vega-data.nodes.guru:3007" "vega-data.bharvest.io:3007"
	defaultVegaGrpcAddresses   = "vega-data.nodes.guru:3007,vega-data.bharvest.io:3007,darling.network:3007,vega-grpc.mainnet.lovali.xyz:3007,grpcvega.gpvalidator.com:3007,vega-mainnet.anyvalid.com:3007" // "tls://vega-mainnet-data-grpc.commodum.io:443,vega-data.nodes.guru:3007,vega-data.bharvest.io:3007,datanode.vega.pathrocknetwork.org:3007,tls://vega-grpc.aurora-edge.com:443,darling.network:3007,tls://grpc.velvet.tm.p2p.org:443,vega-grpc.mainnet.lovali.xyz:3007,grpcvega.gpvalidator.com:3007,vega-mainnet.anyvalid.com:3007"
	defaultBinanceWsAddr       = "wss://stream.binance.com:443/ws"
	defaultWalletServiceAddr   = "http://127.0.0.1:1789"
	defaultWalletPubkey        = ""
	defaultVegaMarkets         = "39410c92ed75c175e6cc572372b8a2adfeb0261a06a4480142b224d87017948c" // "5b05109662e7434fea498c4a1c91d3179b80e9b8950d6106cec60e1f342fc604,2c2ea995d7366e423be7604f63ce047aa7186eb030ecc7b77395eae2fcbffcc5,074c929bba8faeeeba352b2569fc5360a59e12cdcbf60f915b492c4ac228b566"
	defaultBinanceMarkets      = "BTCUSDT,ETHUSDT,LINKUSDT"
	defaultLpMarket            = "39410c92ed75c175e6cc572372b8a2adfeb0261a06a4480142b224d87017948c"
	defaultLpCommitmentSizeUSD = "10000"
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
	// AmendLiquidityCommitment(walletClient, dataClient)

	// time.Sleep(1 * time.Second)

	apiCh := make(chan *ApiState)

	go RunStrategy(walletClient, dataClient, apiCh)

	go StartApi(apiCh)

	gracefulStop := make(chan os.Signal, 1)
	signal.Notify(gracefulStop, syscall.SIGTERM, syscall.SIGINT)
	<-gracefulStop

	// Should we flatten the inventory at shutdown?

	log.Print("Terminating due to user input.")

}
