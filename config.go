package main

import (
	// "fmt"
	"flag"
	"log"
	"os"
)

type Config struct {
	VegaGrpcAddr      string
	BinanceWsAddr     string
	WalletServiceAddr string
	WalletToken       string
	WalletPubkey      string
	VegaMarkets       string
	BinanceMarkets    string
}

func parseFlags() *Config {
	flag.Parse()

	if vegaGrpcAddr = getFlag(vegaGrpcAddr, os.Getenv("VEGAMM_VEGA_GRPC_ADDR")); len(vegaGrpcAddr) <= 0 {
		vegaGrpcAddr = defaultVegaGrpcAddr
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

	return &Config{
		VegaGrpcAddr:      vegaGrpcAddr,
		BinanceWsAddr:     binanceWsAddr,
		WalletServiceAddr: walletServiceAddr,
		WalletToken:       walletToken,
		WalletPubkey:      walletPubkey,
		VegaMarket:        vegaMarket,
		BinanceMarket:     binanceMarket,
	}
}

func getFlag(flag, env string) string {
	if len(flag) <= 0 {
		return env
	}
	return flag
}
