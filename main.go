package main

import (
	"flag"
	"fmt"
	"strings"
	"vega-mm/data-engine"
	"vega-mm/metrics"
	"vega-mm/pow"
	strats "vega-mm/strategies"
	"vega-mm/trading-engine"

	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	// vegaApiPb "code.vegaprotocol.io/vega/protos/vega/api/v1"
	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"
	"github.com/shopspring/decimal"
	// "net/http"
	// _ "net/http/pprof"
)

const (
	defaultAdminPort = 8888

	// -------------------- Mainnet APIs -------------------- \\
	defaultVegaCoreAddrs     = "104.248.164.229:3002,167.71.44.7:3002,164.90.205.53:3002,185.246.86.71:3002,141.95.32.237:3002,176.9.125.110:3002,104.248.40.150:3002,164.92.138.136:3002,65.21.60.252:3002,65.108.226.25:3002,135.181.106.186:3002,167.71.55.128:3002"
	defaultVegaGrpcAddresses = "vega-data.nodes.guru:3007,darling.network:3007,grpcvega.gpvalidator.com:3007,vega-mainnet.anyvalid.com:3007" // vega-grpc.mainnet.lovali.xyz:3007,
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

func GetFairgroundStrategies() []strats.Strategy {

	s := []strats.Strategy{}

	btcMartingaleStrategyOpts := &strats.StrategyOpts[strats.Martingale]{
		General: &strats.GeneralOpts{
			AgentKeyPairIdx:        1,
			VegaMarketId:           "",
			BinanceMarket:          "BTCUSDT",
			NumOrdersPerSide:       5,
			LiquidityCommitment:    true,
			TargetObligationVolume: decimal.NewFromInt(10000),
			TargetVolCoefficient:   decimal.NewFromFloat(1.2),
		},
		Specific: &strats.MartingaleOpts{
			MaxProbabilityOfTrading: decimal.NewFromFloat(0.85),
			OrderSpacing:            decimal.NewFromFloat(0.0005),
			OrderSizeBase:           decimal.NewFromFloat(2),
		},
	}

	btcMartingaleStrategy := strats.NewMartingaleStrategy(btcMartingaleStrategyOpts)

	s = append(s, btcMartingaleStrategy)

	return nil
}

func GetMainnetStrategies() []strats.Strategy {

	s := []strats.Strategy{}

	// btcPerpStrategyOpts := &StrategyOpts{
	// 	marketId:                "4e9081e20e9e81f3e747d42cb0c9b8826454df01899e6027a22e771e19cc79fc",
	// 	binanceMarket:           "BTCUSDT",
	// 	targetObligationVolume:  160000, // Minimum 10k on mainnet (min commitment: 500, stakeToCcyVolume: 20)
	// 	maxProbabilityOfTrading: 0.95,   // Determines where to place the first order in the distribution
	// 	orderSpacing:            0.00035,
	// 	orderSizeBase:           1.8,
	// 	targetVolCoefficient:    1.2, // Aim to quote 1.2x targetObligationVolume on each side
	// 	numOrdersPerSide:        5,
	// }

	// ethPerpStrategyOpts := &StrategyOpts{
	// 	marketId:                "e63a37edae8b74599d976f5dedbf3316af82579447f7a08ae0495a021fd44d13",
	// 	binanceMarket:           "ETHUSDT",
	// 	targetObligationVolume:  150000, // Minimum 10k on mainnet (min commitment: 500, stakeToCcyVolume: 20)
	// 	maxProbabilityOfTrading: 0.95,
	// 	orderSpacing:            0.00035,
	// 	orderSizeBase:           1.8,
	// 	targetVolCoefficient:    1.2,
	// 	numOrdersPerSide:        5,
	// }

	// solPerpStrategyOpts := &StrategyOpts{
	// 	marketId:                "f148741398d6bafafdc384819808a14e07340182455105e280aa0294c92c2e60",
	// 	binanceMarket:           "SOLUSDT",
	// 	targetObligationVolume:  100000, // Minimum 10k on mainnet (min commitment: 500, stakeToCcyVolume: 20)
	// 	maxProbabilityOfTrading: 0.85,
	// 	orderSpacing:            0.0005,
	// 	orderSizeBase:           1.6,
	// 	targetVolCoefficient:    1.2,
	// 	numOrdersPerSide:        7,
	// }

	// linkPerpStrategyOpts := &StrategyOpts{
	// 	marketId:                "74f8bb5c2236dac8f29ee10c18d70d553b8faa180f288b559ef795d0faeb3607",
	// 	binanceMarket:           "LINKUSDT",
	// 	targetObligationVolume:  75000, // Minimum 10k on mainnet (min commitment: 500, stakeToCcyVolume: 20)
	// 	maxProbabilityOfTrading: 0.825,
	// 	orderSpacing:            0.000675,
	// 	orderSizeBase:           1.6,
	// 	targetVolCoefficient:    1.15,
	// 	numOrdersPerSide:        4,
	// }

	// btcMartingaleStrategyOpts := &strats.StrategyOpts[strats.Martingale]{
	// 	General: &strats.GeneralOpts{
	// 		AgentKeyPairIdx:        2,
	// 		VegaMarketId:           "4e9081e20e9e81f3e747d42cb0c9b8826454df01899e6027a22e771e19cc79fc",
	// 		BinanceMarket:          "BTCUSDT",
	// 		NumOrdersPerSide:       6,
	// 		LiquidityCommitment:    false,
	// 		TargetObligationVolume: decimal.NewFromInt(5000),
	// 		TargetVolCoefficient:   decimal.NewFromFloat(1.2),
	// 	},
	// 	Specific: &strats.MartingaleOpts{
	// 		MaxProbabilityOfTrading: decimal.NewFromFloat(0.2),
	// 		OrderSpacing:            decimal.NewFromFloat(0.0005),
	// 		OrderSizeBase:           decimal.NewFromFloat(2),
	// 	},
	// }

	// ethMartingaleStrategyOpts := &strats.StrategyOpts[strats.Martingale]{
	// 	General: &strats.GeneralOpts{
	// 		AgentKeyPairIdx:        2,
	// 		VegaMarketId:           "e63a37edae8b74599d976f5dedbf3316af82579447f7a08ae0495a021fd44d13",
	// 		BinanceMarket:          "ETHUSDT",
	// 		NumOrdersPerSide:       6,
	// 		LiquidityCommitment:    false,
	// 		TargetObligationVolume: decimal.NewFromInt(5000),
	// 		TargetVolCoefficient:   decimal.NewFromFloat(1.2),
	// 	},
	// 	Specific: &strats.MartingaleOpts{
	// 		MaxProbabilityOfTrading: decimal.NewFromFloat(0.2),
	// 		OrderSpacing:            decimal.NewFromFloat(0.0005),
	// 		OrderSizeBase:           decimal.NewFromFloat(2),
	// 	},
	// }

	// solMartingaleStrategyOpts := &strats.StrategyOpts[strats.Martingale]{
	// 	General: &strats.GeneralOpts{
	// 		AgentKeyPairIdx:        3,
	// 		VegaMarketId:           "f148741398d6bafafdc384819808a14e07340182455105e280aa0294c92c2e60",
	// 		BinanceMarket:          "SOLUSDT",
	// 		NumOrdersPerSide:       6,
	// 		LiquidityCommitment:    false,
	// 		TargetObligationVolume: decimal.NewFromInt(5000),
	// 		TargetVolCoefficient:   decimal.NewFromFloat(1.2),
	// 	},
	// 	Specific: &strats.MartingaleOpts{
	// 		MaxProbabilityOfTrading: decimal.NewFromFloat(0.2),
	// 		OrderSpacing:            decimal.NewFromFloat(0.0005),
	// 		OrderSizeBase:           decimal.NewFromFloat(2),
	// 	},
	// }

	// linkMartingaleStrategyOpts := &strats.StrategyOpts[strats.Martingale]{
	// 	General: &strats.GeneralOpts{
	// 		AgentKeyPairIdx:        4,
	// 		VegaMarketId:           "74f8bb5c2236dac8f29ee10c18d70d553b8faa180f288b559ef795d0faeb3607",
	// 		BinanceMarket:          "LINKUSDT",
	// 		NumOrdersPerSide:       6,
	// 		LiquidityCommitment:    false,
	// 		TargetObligationVolume: decimal.NewFromInt(5000),
	// 		TargetVolCoefficient:   decimal.NewFromFloat(1.2),
	// 	},
	// 	Specific: &strats.MartingaleOpts{
	// 		MaxProbabilityOfTrading: decimal.NewFromFloat(0.2),
	// 		OrderSpacing:            decimal.NewFromFloat(0.0005),
	// 		OrderSizeBase:           decimal.NewFromFloat(2),
	// 	},
	// }

	btcAggressiveStrategyOpts := &strats.StrategyOpts[strats.Aggressive]{
		General: &strats.GeneralOpts{
			AgentKeyPairIdx:        1,
			VegaMarketId:           "4e9081e20e9e81f3e747d42cb0c9b8826454df01899e6027a22e771e19cc79fc",
			BinanceMarket:          "BTCUSDT",
			LiquidityCommitment:    false,
			TargetObligationVolume: decimal.NewFromInt(200000),
			TargetVolCoefficient:   decimal.NewFromFloat(1.1),
		},
		Specific: &strats.AggressiveOpts{
			InitialOffset:         decimal.NewFromFloat(0.0125),
			ReduceExposureAsTaker: false,
			ReductionThreshold:    decimal.NewFromInt(0),
			ReductionFactor:       decimal.NewFromFloat(0.15),
			HighAggression:        false,
		},
	}

	ethAggressiveStrategyOpts := &strats.StrategyOpts[strats.Aggressive]{
		General: &strats.GeneralOpts{
			AgentKeyPairIdx:        1,
			VegaMarketId:           "e63a37edae8b74599d976f5dedbf3316af82579447f7a08ae0495a021fd44d13",
			BinanceMarket:          "ETHUSDT",
			LiquidityCommitment:    false,
			TargetObligationVolume: decimal.NewFromInt(200000),
			TargetVolCoefficient:   decimal.NewFromFloat(1.1),
		},
		Specific: &strats.AggressiveOpts{
			InitialOffset:         decimal.NewFromFloat(0.0125),
			ReduceExposureAsTaker: true,
			ReductionThreshold:    decimal.NewFromInt(0),
			ReductionFactor:       decimal.NewFromFloat(0.15),
			HighAggression:        false,
		},
	}

	injAggressiveStrategyOpts := &strats.StrategyOpts[strats.Aggressive]{
		General: &strats.GeneralOpts{
			AgentKeyPairIdx:        3,
			VegaMarketId:           "b0ef037ff334cb83f80897b92ce197b440e27af47e671cd59933595e942abfc9",
			BinanceMarket:          "INJUSDT",
			LiquidityCommitment:    false,
			TargetObligationVolume: decimal.NewFromInt(10000),
			TargetVolCoefficient:   decimal.NewFromFloat(1.1),
		},
		Specific: &strats.AggressiveOpts{
			InitialOffset:         decimal.NewFromFloat(0.01),
			ReduceExposureAsTaker: true,
			ReductionThreshold:    decimal.NewFromInt(0),
			ReductionFactor:       decimal.NewFromFloat(0.125),
			HighAggression:        false,
		},
	}

	snxAggressiveStrategyOpts := &strats.StrategyOpts[strats.Aggressive]{
		General: &strats.GeneralOpts{
			AgentKeyPairIdx:        4,
			VegaMarketId:           "5ec43c5d3570ff001b5072faeeff56b4320124175a76a1b624df80169b1ece5e",
			BinanceMarket:          "SNXUSDT",
			LiquidityCommitment:    true,
			TargetObligationVolume: decimal.NewFromInt(10000),
			TargetVolCoefficient:   decimal.NewFromFloat(1.1),
		},
		Specific: &strats.AggressiveOpts{
			InitialOffset:         decimal.NewFromFloat(0.01),
			ReduceExposureAsTaker: false,
			ReductionThreshold:    decimal.NewFromInt(0),
			ReductionFactor:       decimal.NewFromFloat(0.125),
			HighAggression:        false,
		},
	}

	ldoAggressiveStrategyOpts := &strats.StrategyOpts[strats.Aggressive]{
		General: &strats.GeneralOpts{
			AgentKeyPairIdx:        5,
			VegaMarketId:           "603f891b390fa67ac1a7b8f520c743e776cf58da7b8637e2572d556ba55f2878",
			BinanceMarket:          "LDOUSDT",
			LiquidityCommitment:    true,
			TargetObligationVolume: decimal.NewFromInt(10000),
			TargetVolCoefficient:   decimal.NewFromFloat(1.1),
		},
		Specific: &strats.AggressiveOpts{
			InitialOffset:         decimal.NewFromFloat(0.015),
			ReduceExposureAsTaker: false,
			ReductionThreshold:    decimal.NewFromInt(0),
			ReductionFactor:       decimal.NewFromFloat(0.15),
			HighAggression:        false,
		},
	}

	solAggressiveStrategyOpts := &strats.StrategyOpts[strats.Aggressive]{
		General: &strats.GeneralOpts{
			AgentKeyPairIdx:        6,
			VegaMarketId:           "f148741398d6bafafdc384819808a14e07340182455105e280aa0294c92c2e60",
			BinanceMarket:          "SOLUSDT",
			LiquidityCommitment:    true,
			TargetObligationVolume: decimal.NewFromInt(10000),
			TargetVolCoefficient:   decimal.NewFromFloat(1.1),
		},
		Specific: &strats.AggressiveOpts{
			InitialOffset:         decimal.NewFromFloat(0.01),
			ReduceExposureAsTaker: false,
			ReductionThreshold:    decimal.NewFromInt(0),
			ReductionFactor:       decimal.NewFromFloat(0.125),
			HighAggression:        false,
		},
	}

	eglpPointsStrategyOpts := &strats.StrategyOpts[strats.Points]{
		General: &strats.GeneralOpts{
			AgentKeyPairIdx:        2,
			VegaMarketId:           "fc37a1eedb6e57b86823e2fc42480a0b9236aea556c1d7df49be697a93f8f2a0",
			BinanceMarket:          "EGLPUSDT",
			NoBinance:              true,
			NumOrdersPerSide:       1,
			LiquidityCommitment:    true,
			TargetObligationVolume: decimal.NewFromInt(10000),
			TargetVolCoefficient:   decimal.NewFromFloat(1.15),
		},
		Specific: &strats.PointsOpts{
			InitialOffset:         decimal.NewFromFloat(0.125),
			ReduceExposureAsTaker: false,
			ReductionThreshold:    decimal.NewFromInt(500),
			ReductionFactor:       decimal.NewFromFloat(0.12),
			OrderSpacing:          decimal.NewFromFloat(0.004),
			OrderSizeBase:         decimal.NewFromFloat(1.8),
		},
	}

	btcAggressiveStrategy := strats.NewAggressiveStrategy(btcAggressiveStrategyOpts)
	ethAggressiveStrategy := strats.NewAggressiveStrategy(ethAggressiveStrategyOpts)
	injAggressiveStrategy := strats.NewAggressiveStrategy(injAggressiveStrategyOpts)
	snxAggressiveStrategy := strats.NewAggressiveStrategy(snxAggressiveStrategyOpts)
	ldoAggressiveStrategy := strats.NewAggressiveStrategy(ldoAggressiveStrategyOpts)
	solAggressiveStrategy := strats.NewAggressiveStrategy(solAggressiveStrategyOpts)

	eglpPointsStrategy := strats.NewPointsStrategy(eglpPointsStrategyOpts)

	_ = btcAggressiveStrategy
	_ = ethAggressiveStrategy
	_ = injAggressiveStrategy
	_ = snxAggressiveStrategy
	_ = ldoAggressiveStrategy
	_ = solAggressiveStrategy
	// s = append(s, btcAggressiveStrategy)
	// s = append(s, ethAggressiveStrategy)
	// s = append(s, injAggressiveStrategy)
	// s = append(s, snxAggressiveStrategy)
	// s = append(s, ldoAggressiveStrategy)
	// s = append(s, solAggressiveStrategy)

	_ = eglpPointsStrategy
	// s = append(s, eglpPointsStrategy)

	// btcMartingaleStrategy := strats.NewMartingaleStrategy(btcMartingaleStrategyOpts)
	// ethMartingaleStrategy := strats.NewMartingaleStrategy(ethMartingaleStrategyOpts)
	// solMartingaleStrategy := strats.NewMartingaleStrategy(solMartingaleStrategyOpts)
	// linkMartingaleStrategy := strats.NewMartingaleStrategy(linkMartingaleStrategyOpts)

	// s = append(s, btcMartingaleStrategy)
	// s = append(s, ethMartingaleStrategy)
	// s = append(s, solMartingaleStrategy)
	// s = append(s, linkMartingaleStrategy)

	return s
}

func init() {
	fmt.Println("Initializing..")
	flag.UintVar(&adminPort, "admin-port", defaultAdminPort, "The port for the Admin API")
	flag.StringVar(&vegaCoreAddrs, "vega-core-addresses", defaultVegaCoreAddrs, "Vega core gRPC servers")
	flag.StringVar(&vegaGrpcAddresses, "vega-grpc-addresses", defaultVegaGrpcAddresses, "Vega grpc servers")
	flag.StringVar(&binanceWsAddr, "binance-ws-addr", defaultBinanceWsAddr, "A Binance websocket url")
	flag.StringVar(&walletServiceAddr, "wallet-service-addr", defaultWalletServiceAddr, "A vega wallet service address")
	flag.StringVar(&walletPubkey, "wallet-pubkey", defaultWalletPubkey, "a vega public key")
	flag.StringVar(&binanceMarkets, "binance-markets", defaultBinanceMarkets, "a comma separated list of Binance markets")
	flag.Parse()
}

func main() {

	// // Profiling for debugging
	// go func() {
	// 	http.ListenAndServe("localhost:1111", nil)
	// }()

	// We initialize all the separate engines in the trading system:
	//
	// Generate a broadcast channel for each signer to send signed txs to the data
	// engine, where they are then broadcast to vega core node.
	//
	// Init the metrics server.
	//	- Register prom metrics with registry.
	//	- Return metrics channel.
	//
	// Create strategy definitions
	//	- Hardcoded strategy options.
	//	- Creates a data store for each strategy.
	//
	// Init Trading Engine to register the strategies
	//	- Instantiates agents, deriving corresponding key pairs
	// 	- Registers strategies with agents
	// 	- Should then wait for data streams and proofs of work
	//
	// Init data engine.
	//	- Pass API endpoints from config.
	//	- Registers strategies with the Data Engine.
	//	- Stores pointers to data store for each strategy.
	//	- Opens API conns and streams.
	//	- Filters streams and directs data to corresponding strategy data stores.
	//	- Begins collecting spam statistics for use by the proof of work worker.
	//
	// Init proof of work worker
	// 	- Register agents with worker.
	//	- Start processing spam statistics events from data engine.
	//	- Start generating pows
	//

	// ---------- MAINNET ---------- //
	strategies := GetMainnetStrategies()
	// metricsPort := ":8080"
	// ----------------------------- //

	// ---------- FAIRGROUND ---------- //
	// strategies := GetFairgroundStrategies()
	metricsPort := ":8079"
	// -------------------------------- //

	txBroadcastCh := make(chan *commandspb.Transaction)

	metricsServer := metrics.NewMetricsServer(metricsPort)
	metricsCh := metricsServer.Init()

	tradingEngine := trading.NewEngine().Init(metricsCh)
	tradingEngine.LoadStrategies(strategies, txBroadcastCh)

	recentBlockCh := make(chan *pow.RecentBlock)

	dataEngine := data.NewDataEngine().RegisterStrategies(tradingEngine.GetStrategies())
	dataEngine.Init(binanceWsAddr, strings.Split(vegaCoreAddrs, ","), strings.Split(vegaGrpcAddresses, ","), txBroadcastCh, recentBlockCh)

	worker := pow.NewWorker().Init(recentBlockCh, tradingEngine.GetNumStratsPerAgent(), tradingEngine.GetPowStores())

	worker.Start()

	wg := &sync.WaitGroup{}
	wg.Add(2)
	dataEngine.Start(wg)
	wg.Wait()

	tradingEngine.Start()
	metricsServer.Start()

	gracefulStop := make(chan os.Signal, 1)
	signal.Notify(gracefulStop, syscall.SIGTERM, syscall.SIGINT)
	<-gracefulStop

	// Should we flatten the inventory at shutdown?

	log.Print("Terminating due to user input.")

	// ---------------------------------- OLD ---------------------------------- //

	/*

		// Get config
		config := parseFlags()

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
			maxProbabilityOfTrading: 0.875,  // Determines where to place the first order in the distribution
			orderSpacing:            0.0005,
			orderSizeBase:           1.4,
			targetVolCoefficient:    1.1, // Aim to quote 1.1x targetObligationVolume on each side
			numOrdersPerSide:        7,
		}

		ethPerpStrategyOpts := &StrategyOpts{
			marketId:                "e63a37edae8b74599d976f5dedbf3316af82579447f7a08ae0495a021fd44d13",
			binanceMarket:           "ETHUSDT",
			targetObligationVolume:  140000, // Minimum 10k on mainnet (min commitment: 500, stakeToCcyVolume: 20)
			maxProbabilityOfTrading: 0.875,
			orderSpacing:            0.0005,
			orderSizeBase:           1.4,
			targetVolCoefficient:    1.1,
			numOrdersPerSide:        7,
		}

		solPerpStrategyOpts := &StrategyOpts{
			marketId:                "f148741398d6bafafdc384819808a14e07340182455105e280aa0294c92c2e60",
			binanceMarket:           "SOLUSDT",
			targetObligationVolume:  75000, // Minimum 10k on mainnet (min commitment: 500, stakeToCcyVolume: 20)
			maxProbabilityOfTrading: 0.825,
			orderSpacing:            0.00085,
			orderSizeBase:           1.75,
			targetVolCoefficient:    1.1,
			numOrdersPerSide:        11,
		}

		linkPerpStrategyOpts := &StrategyOpts{
			marketId:                "74f8bb5c2236dac8f29ee10c18d70d553b8faa180f288b559ef795d0faeb3607",
			binanceMarket:           "LINKUSDT",
			targetObligationVolume:  75000, // Minimum 10k on mainnet (min commitment: 500, stakeToCcyVolume: 20)
			maxProbabilityOfTrading: 0.825,
			orderSpacing:            0.00065,
			orderSizeBase:           1.75,
			targetVolCoefficient:    1.1,
			numOrdersPerSide:        8,
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

	*/

}
