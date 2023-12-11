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
	defaultAdminPort = 8888

	// -------------------- Mainnet APIs -------------------- \\
	defaultVegaCoreAddrs     = "104.248.164.229:3002,167.71.44.7:3002,164.90.205.53:3002,185.246.86.71:3002,141.95.32.237:3002,176.9.125.110:3002,104.248.40.150:3002,164.92.138.136:3002,65.21.60.252:3002,65.108.226.25:3002,135.181.106.186:3002,167.71.55.128:3002"
	defaultVegaGrpcAddresses = "vega-data.nodes.guru:3007,darling.network:3007,vega-grpc.mainnet.lovali.xyz:3007,grpcvega.gpvalidator.com:3007,vega-mainnet.anyvalid.com:3007"
	// ------------------------------------------------------ \\

	// -------------------- Fairground APIs -------------------- \\
	// defaultVegaCoreAddrs     = "api.n00.testnet.vega.rocks:3002,api.n06.testnet.vega.rocks:3002,api.n07.testnet.vega.rocks:3002,api.n08.testnet.vega.rocks:3002,api.n09.testnet.vega.rocks:3002"
	// defaultVegaGrpcAddresses = "api.n00.testnet.vega.rocks:3007,api.n06.testnet.vega.rocks:3007,api.n07.testnet.vega.rocks:3007,api.n08.testnet.vega.rocks:3007,api.n09.testnet.vega.rocks:3007"
	// --------------------------------------------------------- \\

	defaultBinanceWsAddr     = "wss://stream.binance.com:443/ws"
	defaultWalletServiceAddr = "http://127.0.0.1:1789" // Note: Fairground wallet runs on port 1788
	defaultWalletPubkey      = ""
	defaultBinanceMarkets    = "BTCUSDT,ETHUSDT,LINKUSDT"
)

var (
	adminPort         uint
	vegaCoreAddrs     string
	vegaGrpcAddresses string
	binanceWsAddr     string
	walletServiceAddr string
	walletPubkey      string
	binanceMarkets    string
)

func init() {
	fmt.Println("Initializing..")
	flag.UintVar(&adminPort, "admin-port", defaultAdminPort, "The port for the Admin API")
	flag.StringVar(&vegaCoreAddrs, "vega-core-addresses", defaultVegaCoreAddrs, "Vega core gRPC servers")
	flag.StringVar(&vegaGrpcAddresses, "vega-grpc-addresses", defaultVegaGrpcAddresses, "Vega grpc servers")
	flag.StringVar(&binanceWsAddr, "binance-ws-addr", defaultBinanceWsAddr, "A Binance websocket url")
	flag.StringVar(&walletServiceAddr, "wallet-service-addr", defaultWalletServiceAddr, "A vega wallet service address")
	flag.StringVar(&walletPubkey, "wallet-pubkey", defaultWalletPubkey, "a vega public key")
	flag.StringVar(&binanceMarkets, "binance-markets", defaultBinanceMarkets, "a comma separated list of Binance markets")
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
	// mnemonic, err := os.ReadFile(fmt.Sprintf("%v/.config/vega-mm-fairground/embedded-wallet/mnemonic.txt", homePath))
	mnemonic, err := os.ReadFile(fmt.Sprintf("%v/.config/vega-mm/embedded-wallet/words.txt", homePath))
	if err != nil {
		log.Fatalf("Failed to read mnemonic from file: %v", err)
	}

	embeddedWallet := newWallet(string(mnemonic))
	agent := NewAgent(embeddedWallet, config).(*agent)

	btcPerpStrategyOpts := &StrategyOpts{
		marketId:                "4e9081e20e9e81f3e747d42cb0c9b8826454df01899e6027a22e771e19cc79fc",
		binanceMarket:           "BTCUSDT",
		targetObligationVolume:  150000, // Minimum 10k on mainnet (min commitment: 500, stakeToCcyVolume: 20)
		maxProbabilityOfTrading: 0.825,  // Determines where to place the first order in the distribution
		orderSpacing:            0.00055,
		orderSizeBase:           1.65,
		targetVolCoefficient:    1.15, // Aim to quote 1.1x targetObligationVolume on each side
		numOrdersPerSide:        10,
	}

	ethPerpStrategyOpts := &StrategyOpts{
		marketId:                "e63a37edae8b74599d976f5dedbf3316af82579447f7a08ae0495a021fd44d13",
		binanceMarket:           "ETHUSDT",
		targetObligationVolume:  140000, // Minimum 10k on mainnet (min commitment: 500, stakeToCcyVolume: 20)
		maxProbabilityOfTrading: 0.825,
		orderSpacing:            0.0006,
		orderSizeBase:           1.8,
		targetVolCoefficient:    1.1,
		numOrdersPerSide:        10,
	}

	solPerpStrategyOpts := &StrategyOpts{
		marketId:                "f148741398d6bafafdc384819808a14e07340182455105e280aa0294c92c2e60",
		binanceMarket:           "SOLUSDT",
		targetObligationVolume:  75000, // Minimum 10k on mainnet (min commitment: 500, stakeToCcyVolume: 20)
		maxProbabilityOfTrading: 0.825,
		orderSpacing:            0.001,
		orderSizeBase:           1.75,
		targetVolCoefficient:    1.1,
		numOrdersPerSide:        10,
	}

	linkPerpStrategyOpts := &StrategyOpts{
		marketId:                "74f8bb5c2236dac8f29ee10c18d70d553b8faa180f288b559ef795d0faeb3607",
		binanceMarket:           "LINKUSDT",
		targetObligationVolume:  50000, // Minimum 10k on mainnet (min commitment: 500, stakeToCcyVolume: 20)
		maxProbabilityOfTrading: 0.85,
		orderSpacing:            0.00075,
		orderSizeBase:           1.75,
		targetVolCoefficient:    1.15,
		numOrdersPerSide:        12,
	}

	btcPerpStrategy := NewStrategy(btcPerpStrategyOpts)
	ethPerpStrategy := NewStrategy(ethPerpStrategyOpts)
	solPerpStrategy := NewStrategy(solPerpStrategyOpts)
	linkPerpStrategy := NewStrategy(linkPerpStrategyOpts)

	agent.RegisterStrategy(btcPerpStrategy)
	agent.RegisterStrategy(ethPerpStrategy)
	agent.RegisterStrategy(solPerpStrategy)
	agent.RegisterStrategy(linkPerpStrategy)

	var wg sync.WaitGroup
	wg.Add(1)

	go agent.signer.RunVegaCoreReconnectHandler()

	go agent.binanceClient.RunBinanceReconnectHandler()
	go agent.binanceClient.StreamBinanceData()

	go agent.VegaClient().RunVegaClientReconnectHandler()
	go agent.VegaClient().StreamVegaData(&wg)

	wg.Wait()

	agent.LoadDecimals()

	agent.UpdateLiquidityCommitment(btcPerpStrategy)
	agent.UpdateLiquidityCommitment(ethPerpStrategy)
	agent.UpdateLiquidityCommitment(solPerpStrategy)
	agent.UpdateLiquidityCommitment(linkPerpStrategy)

	metricsCh := make(chan *MetricsState)

	go agent.RunStrategy(btcPerpStrategy, metricsCh)
	go agent.RunStrategy(ethPerpStrategy, metricsCh)
	go agent.RunStrategy(solPerpStrategy, metricsCh)
	go agent.RunStrategy(linkPerpStrategy, metricsCh)

	go StartMetricsApi(metricsCh)

	gracefulStop := make(chan os.Signal, 1)
	signal.Notify(gracefulStop, syscall.SIGTERM, syscall.SIGINT)
	<-gracefulStop

	// Should we flatten the inventory at shutdown?

	log.Print("Terminating due to user input.")

}
