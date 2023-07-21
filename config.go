package main

import (
	// "fmt"
	"flag"
	"log"
	"os"
)

type Config struct {
	VegaGrpcAddr		string
	BinanceWsAddr		string
	WalletServiceAddr 	string
	WalletToken			string
	WalletPubkey		string
	VegaMarket			string
	BinanceMarket		string
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
	// if walletToken = getFlag(walletToken, os.Getenv("VEGAMM_WALLET_TOKEN")); len(walletToken) <= 0 {
	// 	log.Fatal("Error: Missing -wallet-token flag")
	// }
	// if walletPubkey = getFlag(walletPubkey, os.Getenv("VEGAMM_WALLET_PUBKEY")); len(walletPubkey) <= 0 {
	// 	log.Fatal("Error: Missing -wallet-pubkey flag")
	// }
	if vegaMarket = getFlag(vegaMarket, os.Getenv("VEGAMM_VEGA_MARKET")); len(vegaMarket) <= 0 {
		log.Fatal("Error: Missing -vega-market flag")
	}
	if binanceMarket = getFlag(binanceMarket, os.Getenv("VEGAMM_BINANCE_MARKET")); len(binanceMarket) <= 0 {
		log.Fatal("Error: Missing -binance-market flag")
	}

	return &Config{
		VegaGrpcAddr: vegaGrpcAddr,
		BinanceWsAddr: binanceWsAddr,
		WalletServiceAddr: walletServiceAddr,
		WalletToken: walletToken,
		WalletPubkey: walletPubkey,
		VegaMarket: vegaMarket,
		BinanceMarket: binanceMarket,
	}
}

func getFlag(flag, env string) string {
	if len(flag) <= 0 {
		return env
	}
	return flag
}