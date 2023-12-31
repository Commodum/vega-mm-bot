package data

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
	"vega-mm/stores"

	"github.com/gorilla/websocket"
	"github.com/shopspring/decimal"
	"golang.org/x/exp/maps"
)

type BinanceClient struct {
	wsAddr       string
	storesMap    map[binanceTicker][]*stores.BinanceStore
	reconnChan   chan struct{}
	reconnecting bool
}

func NewBinanceClient(wsAddr string, stores map[binanceTicker][]*stores.BinanceStore) *BinanceClient {
	return &BinanceClient{
		wsAddr:     wsAddr,
		storesMap:  stores,
		reconnChan: make(chan struct{}),
	}
}

func (b *BinanceClient) handleBinanceReconnect() {

	log.Printf("Attempting to reconnect to Binance websocket...")
	if b.reconnecting {
		return
	}
	b.reconnecting = true
	b.reconnChan <- struct{}{}
}

func (b *BinanceClient) RunBinanceReconnectHandler() {

	for {
		select {
		case <-b.reconnChan:

			b.StreamBinanceData()
			b.reconnecting = false

			// After we finish reconnecting mark the binance datastores as not stale.
			for _, stores := range b.storesMap {
				for _, store := range stores {
					store.SetIsStale(false)
				}
			}
		}
	}
}

func (b *BinanceClient) StreamBinanceData() {

	delayReconnect := func() {
		for _, stores := range b.storesMap {
			for _, store := range stores {
				store.SetIsStale(true) // Mark binance data as stale as we might not be able to reconnect for a while
			}
		}
		time.Sleep(30 * time.Second)
	}

	conn, _, err := websocket.DefaultDialer.Dial(b.wsAddr, nil)
	if err != nil {
		log.Printf("Could not dial binance websocket address. Will try again later.")
		b.reconnecting = false
		delayReconnect()
		b.handleBinanceReconnect()
		return
	}

	// We might need to make separate requests here if we have too many tickers in a single
	// "SUBSCRIBE" request.
	reqParams := []string{}
	// for binanceMarket := range b.markets {
	// 	reqParams = append(reqParams, fmt.Sprintf("%s@ticker", strings.ToLower(binanceMarket)))
	// }
	for binanceMarket := range maps.Keys(b.storesMap) {
		reqParams = append(reqParams, fmt.Sprintf("%s@bookTicker", strings.ToLower(string(binanceMarket))))
	}

	req := struct {
		ID     uint     `json:"id"`
		Method string   `json:"method"`
		Params []string `json:"params"`
	}{
		ID:     1,
		Method: "SUBSCRIBE",
		Params: reqParams,
	}

	reqBytes, _ := json.Marshal(req)
	err = conn.WriteMessage(websocket.TextMessage, reqBytes)
	if err != nil {
		log.Printf("Could not write message on binance websocket. Will try again later.")
		b.reconnecting = false
		delayReconnect()
		b.handleBinanceReconnect()
		return
	}

	// Discard first message
	_, _, err = conn.ReadMessage()
	if err != nil {
		log.Printf("Could not read message from binance websocket. Will try again later.")
		conn.Close()
		b.reconnecting = false
		delayReconnect()
		b.handleBinanceReconnect()
		return
	}

	res := struct {
		Symbol   string          `json:"s"`
		AskPrice decimal.Decimal `json:"a"`
		BidPrice decimal.Decimal `json:"b"`
		// Unused, present for correct unmarshalling
		NotA string `json:"A"`
		NotB string `json:"B"`
	}{}

	go func() {
		defer conn.Close()
		rateCalc := struct {
			mu         sync.RWMutex
			startTime  int64
			msgCounter int64
		}{
			mu:         sync.RWMutex{},
			startTime:  time.Now().UnixMicro(),
			msgCounter: 0,
		}
		go func() {
			for range time.NewTicker(time.Second).C {
				rateCalc.mu.Lock()
				msgRate := float64(rateCalc.msgCounter*1000000) / float64((time.Now().UnixMicro() - rateCalc.startTime))
				log.Printf("Binance message rate: %v", strconv.FormatFloat(msgRate, 'f', 2, 64))

				rateCalc.startTime = time.Now().UnixMicro()
				rateCalc.msgCounter = 0
				rateCalc.mu.Unlock()
			}
		}()
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				log.Printf("Could not read message from Binance websocket: %v\n", err)
				b.handleBinanceReconnect()
				return
			}

			// log.Printf("Response from Binance Websocket: %v\n", string(msg))

			err = json.Unmarshal(msg, &res)
			if err != nil {
				log.Printf("Failed to unmarshal response from Binance websocket: %v\n", err)
				b.handleBinanceReconnect()
				return
			}

			// if res.E != "24hrTicker" {
			// 	log.Printf("Unknown event recieved from Binance websocket...\n")
			// 	continue
			// }

			// log.Printf("strats: %+v\n", b.agent.strategies)
			// log.Printf("b.markets: %+v\n", b.markets)
			// log.Printf("res.Symbol: %+v\n", res.Symbol)
			// log.Printf("Binance best bid and ask: %+v, %+v\n", res.BidPrice, res.AskPrice)

			rateCalc.mu.Lock()
			rateCalc.msgCounter++
			rateCalc.mu.Unlock()

			for _, store := range b.storesMap[binanceTicker(res.Symbol)] {
				store.SetBestBidAndAsk(res.BidPrice, res.AskPrice)
			}
		}
	}()
}
