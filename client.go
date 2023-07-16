package main

import (
	"fmt"
	"log"
	"encoding/json"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/shopspring/decimal"
	apipb "code.vegaprotocol.io/vega/protos/data-node/api/v2"
	vegapb "code.vegaprotocol.io/vega/protos/vega"
	"google.golang.org/grpc"
)

type VegaClient struct {
	grpcAddr 	string
	vegaMarket 	string
}

type BinanceClient struct {
	wsAddr 			string
	binanceMarket 	string
}

type DataClient struct {
	b *BinanceClient
	v *VegaClient
	c *Config
	s *DataStore
}

func newDataClient(config *Config, store *DataStore) *DataClient {
	return &DataClient{
		b: &BinanceClient{
			wsAddr: config.BinanceWsAddr,
			binanceMarket: config.BinanceMarket,
		},
		v: &VegaClient{
			grpcAddr: config.VegaGrpcAddr,
			vegaMarket: config.VegaMarket,
			svc: apipb.TradingDataServiceClient,
		},
		c: config,
		s: store,
	}
}


func (d *DataClient) streamBinanceData() {

	conn, _, err := websocket.DefaultDialer.Dial(d.b.wsAddr, nil);
	if err != nil {
		log.Fatal("Dial Error: ", err)
	}
	defer conn.Close()

	req := struct{
		Id 		uint		`json:"id"`
		Method 	string		`json:"method"`
		Params 	[]string	`json:"params"`
	}{
		Id: 1,
		Method: "SUBSCRIBE",
		Params: []string{fmt.Sprintf("%s@ticker", strings.ToLower(d.c.BinanceMarket))},
	}

	out, _ := json.Marshal(req)

	if err = conn.WriteMessage(websocket.TextMessage, out); err != nil {
		log.Fatalf("Could not write on Binnace Websocket: %v", err)
	}

	_, _, err = conn.ReadMessage()
	if err != nil {
		log.Fatalf("Could not read from Binance websocket: %v", err)
	}

	response := struct {
		Type 		string			`json:"e"`
		AskPrice	decimal.Decimal `json:"a"`
		BidPrice	decimal.Decimal `json:"b"`
		// Dummy properties to prevent unmarshalling capitalised property
		// names into lowercase properties.
		Timestamp 	uint64	`json:"E"`
		NotA		string	`json:"A"`
		NotB		string	`json:"B"`
	}{}

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			fmt.Println(string(out))
			log.Fatalf("Could not read message from websocket: %v", err)
		}

		err = json.Unmarshal(msg, &response)
		if err != nil {
			log.Fatalf("Could not unmarshal websocket response: %v - %v", err, msg)
		}

		// fmt.Println(string(msg))
		fmt.Printf("Response Data: %+v\n", response)

		// Check content of response.

		// Set value in store
		d.s.b.Set(response.BidPrice.Copy(), response.AskPrice.Copy())
	}

}

// type VegaStore struct {
// 	mu sync.RWMutex

// 	marketId string
// 	market *vegapb.Market
// 	marketData *vegapb.MarketData
// 	accounts map[string]*apipb.AccountBalance
// 	orders map[string]*vegapb.Order
// 	position *vegapb.Position
// }

func (d *DataClient) streamVegaData() {

	conn, err := grpc.Dial(d.c.vegaGrpcAddr , grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Could not open connection to datanode: %v", err);
	}

	d.v.svc = apipb.NewTradingDataServiceClient(conn)

	// Load initial data
	d.v.loadMarket(d.c, d.s)
	d.v.loadMarketData(d.c, d.s)
	d.v.loadAccounts(d.c, d.s)
	d.v.loadOrders(d.c, d.s)
	d.v.loadPosition(d.c, d.s)

	// Start streams
	go d.v.streamMarketData(d.c, d.s)
	go d.v.streamAccounts(d.c, d.s)
	go d.v.streamOrders(d.c, d.s)
	go d.v.streamPosition(d.c, d.s)

}

func (v *VegaClient) loadMarket(config *Config, store *DataStore) {

	res, err := v.svc.GetMarket(context.Background(), &apipb.GetMarketRequest{MarketId: config.VegaMarket})
	if err != nil {
		log.Fatalf("Couldn't load Vega market: %v", err)
	}

	v.store.SetMarket(resp.Market)
}