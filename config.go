package main

import (
	// "fmt"
	"encoding/json"
	"flag"
	"io"
	"log"
	"os"
)

type Config struct {
	VegaCoreAddrs       string
	VegaGrpcAddr        string
	VegaGrpcAddresses   string
	BinanceWsAddr       string
	WalletServiceAddr   string
	WalletToken         string
	WalletPubkey        string
	VegaMarkets         string
	BinanceMarkets      string
	LpMarket            string
	LpCommitmentSizeUSD string
}

type AgentConfig struct {
	AgentId int    `json:"agentId"`
	Pubkey  string `json:"pubkey"`
}

type StrategyConfig struct {
	MarketId                string  `json:"marketId"`
	TargetObligationVolume  int64   `json:"targetObligationVolume"`
	MaxProbabilityOfTrading float64 `json:"maxProbabilityOfTrading"`
	OrderSpacing            float64 `json:"orderSpacing"`
	OrderSizeBase           float64 `json:"orderSizeBase"`
	TargetVolCoefficient    float64 `json:"targetVolCoefficient"`
	NumOrdersPerSide        int     `json:"numOrdersPerSide"`
}

type JsonConfig struct {
	strategies []StrategyConfig
}

func parseFlags() *Config {
	flag.Parse()

	if vegaCoreAddrs = getFlag(vegaCoreAddrs, os.Getenv("VEGAMM_VEGA_CORE_ADDRS")); len(vegaCoreAddrs) <= 0 {
		vegaCoreAddrs = defaultVegaCoreAddrs
	}
	if vegaGrpcAddr = getFlag(vegaGrpcAddr, os.Getenv("VEGAMM_VEGA_GRPC_ADDR")); len(vegaGrpcAddr) <= 0 {
		vegaGrpcAddr = defaultVegaGrpcAddr
	}
	if vegaGrpcAddresses = getFlag(vegaGrpcAddresses, os.Getenv("VEGAMM_VEGA_GRPC_ADDRESSES")); len(vegaGrpcAddresses) <= 0 {
		vegaGrpcAddresses = defaultVegaGrpcAddresses
	}
	if binanceWsAddr = getFlag(binanceWsAddr, os.Getenv("VEGAMM_BINANCE_WS_ADDR")); len(binanceWsAddr) <= 0 {
		binanceWsAddr = defaultBinanceWsAddr
	}
	if walletServiceAddr = getFlag(walletServiceAddr, os.Getenv("VEGAMM_WALLET_SERVICE_ADDR")); len(walletServiceAddr) <= 0 {
		walletServiceAddr = defaultWalletServiceAddr
	}
	if walletToken = getFlag(walletToken, os.Getenv("VEGAMM_WALLET_TOKEN")); len(walletToken) <= 0 {
		log.Fatal("Error: Missing -wallet-token flag")
	}
	if walletPubkey = getFlag(walletPubkey, os.Getenv("VEGAMM_WALLET_PUBKEY")); len(walletPubkey) <= 0 {
		log.Fatal("Error: Missing -wallet-pubkey flag")
	}
	if vegaMarkets = getFlag(vegaMarkets, os.Getenv("VEGAMM_VEGA_MARKETS")); len(vegaMarkets) <= 0 {
		log.Fatal("Error: Missing -vega-markets flag")
	}
	if binanceMarkets = getFlag(binanceMarkets, os.Getenv("VEGAMM_BINANCE_MARKETS")); len(binanceMarkets) <= 0 {
		log.Fatal("Error: Missing -binance-markets flag")
	}
	if lpMarket = getFlag(lpMarket, os.Getenv("VEGAMM_LP_MARKET")); len(lpMarket) <= 0 {
		log.Fatal("Error: Missing -lp-market flag")
	}
	if targetCommitmentSize = getFlag(targetCommitmentSize, os.Getenv("VEGAMM_LP_COMMITMENT_SIZE")); len(targetCommitmentSize) <= 0 {
		log.Fatal("Error: Missing -lp-commitment-size flag")
	}

	return &Config{
		VegaCoreAddrs:       vegaCoreAddrs,
		VegaGrpcAddr:        vegaGrpcAddr,
		VegaGrpcAddresses:   vegaGrpcAddresses,
		BinanceWsAddr:       binanceWsAddr,
		WalletServiceAddr:   walletServiceAddr,
		WalletToken:         walletToken,
		WalletPubkey:        walletPubkey,
		VegaMarkets:         vegaMarkets,
		BinanceMarkets:      binanceMarkets,
		LpMarket:            lpMarket,
		LpCommitmentSizeUSD: targetCommitmentSize,
	}
}

func getFlag(flag, env string) string {
	if len(flag) <= 0 {
		return env
	}
	return flag
}

// We might want to load our strategies from a JSON file. We also want to update this file with any changes
// to the strategy that are triggered via the admin API, which we will build later.
func loadJsonConfig() *StrategyOpts {

	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	log.Println(cwd)

	jsonConfig := &JsonConfig{}

	jsonFile, err := os.Open("config.json")
	if err != nil {
		log.Fatalf("Could not read from json file: %v", err)
	}
	defer jsonFile.Close()

	bytes, _ := io.ReadAll(jsonFile)

	json.Unmarshal(bytes, jsonConfig)

	log.Printf("Unmarshalled json into config: %v+\n", jsonConfig)

	os.Exit(0)

	strats := jsonConfig.strategies

	return &StrategyOpts{
		marketId:                strats[0].MarketId,
		targetObligationVolume:  strats[0].TargetObligationVolume,
		maxProbabilityOfTrading: strats[0].MaxProbabilityOfTrading,
		orderSpacing:            strats[0].OrderSpacing,
		orderSizeBase:           strats[0].OrderSizeBase,
		targetVolCoefficient:    strats[0].TargetVolCoefficient,
		numOrdersPerSide:        strats[0].NumOrdersPerSide,
	}
}
