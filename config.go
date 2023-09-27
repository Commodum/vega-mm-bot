package main

import (
	// "fmt"
	"flag"
	"log"
	"os"
)

type Config struct {
	VegaGrpcAddr        string
	VegaGrpcAddresses	string
	BinanceWsAddr       string
	WalletServiceAddr   string
	WalletToken         string
	WalletPubkey        string
	VegaMarkets         string
	BinanceMarkets      string
	LpMarket            string
	LpCommitmentSizeUSD string
}

func parseFlags() *Config {
	flag.Parse()

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
	if lpCommitmentSizeUSD = getFlag(lpCommitmentSizeUSD, os.Getenv("VEGAMM_LP_COMMITMENT_SIZE")); len(lpCommitmentSizeUSD) <= 0 {
		log.Fatal("Error: Missing -lp-commitment-size flag")
	}

	return &Config{
		VegaGrpcAddr:        vegaGrpcAddr,
		VegaGrpcAddresses:	 vegaGrpcAddresses,
		BinanceWsAddr:       binanceWsAddr,
		WalletServiceAddr:   walletServiceAddr,
		WalletToken:         walletToken,
		WalletPubkey:        walletPubkey,
		VegaMarkets:         vegaMarkets,
		BinanceMarkets:      binanceMarkets,
		LpMarket:            lpMarket,
		LpCommitmentSizeUSD: lpCommitmentSizeUSD,
	}
}

func getFlag(flag, env string) string {
	if len(flag) <= 0 {
		return env
	}
	return flag
}
