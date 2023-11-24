package main

import (
	"flag"
	"fmt"

	// "io"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	// "time"
	// wallet "github.com/jeremyletang/vega-go-sdk/wallet"
	// "net/http"
	// _ "net/http/pprof"
)

// Note: Below values configured for Fairground
const (
	defaultAdminPort            = 8080
	defaultVegaGrpcAddr         = "api.n00.testnet.vega.rocks:3007"
	defaultVegaGrpcAddresses    = "api.n00.testnet.vega.rocks:3007,api.n06.testnet.vega.rocks:3007,api.n07.testnet.vega.rocks:3007,api.n08.testnet.vega.rocks:3007,api.n09.testnet.vega.rocks:3007"
	defaultBinanceWsAddr        = "wss://stream.binance.com:443/ws"
	defaultWalletServiceAddr    = "http://127.0.0.1:1788" // Note: Fairground wallet running on port 1788
	defaultWalletPubkey         = ""
	defaultVegaMarkets          = "69abf5c456c20f4d189cea79a11dfd6b0958ead58ab34bd66f73eea48aee600c"
	defaultBinanceMarkets       = "BTCUSDT,ETHUSDT,LINKUSDT"
	defaultLpMarket             = "69abf5c456c20f4d189cea79a11dfd6b0958ead58ab34bd66f73eea48aee600c"
	defaultTargetCommitmentSize = "5000"
	defaultVegaCoreAddrs        = "api.n00.testnet.vega.rocks:3002,api.n06.testnet.vega.rocks:3002,api.n07.testnet.vega.rocks:3002,api.n08.testnet.vega.rocks:3002,api.n09.testnet.vega.rocks:3002"
)

var (
	adminPort            uint
	vegaCoreAddrs        string
	vegaGrpcAddr         string
	vegaGrpcAddresses    string
	binanceWsAddr        string
	walletServiceAddr    string
	walletToken          string
	walletPubkey         string
	vegaMarkets          string
	binanceMarkets       string
	lpMarket             string
	targetCommitmentSize string
)

func init() {
	fmt.Println("Initializing..")
	flag.UintVar(&adminPort, "admin-port", defaultAdminPort, "The port for the Admin API")
	flag.StringVar(&vegaCoreAddrs, "vega-core-addresses", defaultVegaCoreAddrs, "Vega core gRPC servers")
	flag.StringVar(&vegaGrpcAddr, "vega-grpc-addr", defaultVegaGrpcAddr, "A vega grpc server")
	flag.StringVar(&vegaGrpcAddresses, "vega-grpc-addresses", defaultVegaGrpcAddresses, "Vega grpc servers")
	flag.StringVar(&binanceWsAddr, "binance-ws-addr", defaultBinanceWsAddr, "A Binance websocket url")
	flag.StringVar(&walletServiceAddr, "wallet-service-addr", defaultWalletServiceAddr, "A vega wallet service address")
	flag.StringVar(&walletToken, "wallet-token", "", "a vega wallet token (for info see vega wallet token-api -h)")
	flag.StringVar(&walletPubkey, "wallet-pubkey", defaultWalletPubkey, "a vega public key")
	flag.StringVar(&vegaMarkets, "vega-markets", defaultVegaMarkets, "a comma separated list of market IDs")
	flag.StringVar(&binanceMarkets, "binance-markets", defaultBinanceMarkets, "a comma separated list of Binance markets")
	flag.StringVar(&lpMarket, "lp-market", defaultLpMarket, "The Vega market to submit an LP commitment to")
	flag.StringVar(&targetCommitmentSize, "lp-commitment-size", defaultTargetCommitmentSize, "The size of the LP commitment in USD")
}

func main() {

	// // Profiling for debugging
	// go func() {
	// 	http.ListenAndServe("localhost:1111", nil)
	// }()

	// Get config
	config := parseFlags()

	// walletClient, err := wallet.NewClient(defaultWalletServiceAddr, config.WalletToken)
	// // _, err := wallet.NewClient(defaultWalletServiceAddr, config.WalletToken)
	// if err != nil {
	// 	log.Fatalf("Could not connect to wallet: %v", err)
	// }

	// agent := NewAgent(walletClient, config)

	// mnemonic := os.Getenv("FAIRGROUND_MNEMONIC")
	homePath := os.Getenv("HOME")
	mnemonic, err := os.ReadFile(fmt.Sprintf("%v/.config/vega-mm-fairground/embedded-wallet/mnemonic.txt", homePath))
	if err != nil {
		log.Fatalf("Failed to read mnemonic from file: %v", err)
	}

	embeddedWallet := newWallet(string(mnemonic))
	agent := NewAgent(embeddedWallet, config).(*agent)

	btcPerpStrategyOpts := &StrategyOpts{
		marketId:                "61e36f238a2fce9bd75a8dc38a92ac216efa1303c788605b2a00272e0a24a767",
		targetObligationVolume:  5000,
		maxProbabilityOfTrading: 0.9, // Determines where to place the first order in the distribution
		orderSpacing:            0.001,
		orderSizeBase:           2.0,
		targetVolCoefficient:    1.25, // Aim to quote 1.25x targetObligationVolume on each side
		numOrdersPerSide:        10,
	}

	ethPerpStrategyOpts := &StrategyOpts{
		marketId:                "a79834ef7d9019a60821d4962fc2561663a2558011dc9ed972ab8b01381e8d10",
		targetObligationVolume:  5000,
		maxProbabilityOfTrading: 0.9,
		orderSpacing:            0.001,
		orderSizeBase:           2.0,
		targetVolCoefficient:    1.25,
		numOrdersPerSide:        10,
	}

	btcPerpStrategy := NewStrategy(btcPerpStrategyOpts)
	ethPerpStrategy := NewStrategy(ethPerpStrategyOpts)

	agent.RegisterStrategy(btcPerpStrategy)
	agent.RegisterStrategy(ethPerpStrategy)

	var wg sync.WaitGroup
	wg.Add(1)

	go agent.VegaClient().RunVegaClientReconnectHandler()
	go agent.VegaClient().StreamVegaData(&wg)

	wg.Wait()

	agent.LoadDecimals()

	agent.UpdateLiquidityCommitment(btcPerpStrategy)
	agent.UpdateLiquidityCommitment(ethPerpStrategy)

	metricsCh := make(chan *MetricsState)

	go agent.RunStrategy(btcPerpStrategy, metricsCh)
	go agent.RunStrategy(ethPerpStrategy, metricsCh)

	go StartMetricsApi(metricsCh)

	gracefulStop := make(chan os.Signal, 1)
	signal.Notify(gracefulStop, syscall.SIGTERM, syscall.SIGINT)
	<-gracefulStop

	// Should we flatten the inventory at shutdown?

	log.Print("Terminating due to user input.")

}
