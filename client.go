package main

import (
	"fmt"
	"log"
	"encoding/json"
	"strings"
	"context"

	"github.com/davecgh/go-spew/spew"
	"github.com/gorilla/websocket"
	"github.com/shopspring/decimal"
	apipb "code.vegaprotocol.io/vega/protos/data-node/api/v2"
	vegapb "code.vegaprotocol.io/vega/protos/vega"
	"google.golang.org/grpc"
)

type VegaClient struct {
	grpcAddr 	string
	vegaMarket 	string
	svc 		apipb.TradingDataServiceClient
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

	conn, err := grpc.Dial(d.c.VegaGrpcAddr , grpc.WithInsecure())
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

	spew.Dump(d.s.v)

	// Start streams
	go d.v.streamMarketData(d.c, d.s)
	go d.v.streamAccounts(d.c, d.s)
	go d.v.streamOrders(d.c, d.s)
	go d.v.streamPosition(d.c, d.s)

}

func (v *VegaClient) loadMarket(config *Config, store *DataStore) {

	fmt.Println(&apipb.GetMarketRequest{MarketId: config.VegaMarket})
	res, err := v.svc.GetMarket(context.Background(), &apipb.GetMarketRequest{MarketId: config.VegaMarket})
	if err != nil {
		log.Fatalf("Couldn't load Vega market: %v", err)
	}

	store.v.SetMarket(res.Market)
}

func (v *VegaClient) loadMarketData(config *Config, store *DataStore) {

	res, err := v.svc.GetLatestMarketData(context.Background(), &apipb.GetLatestMarketDataRequest{MarketId: config.VegaMarket})
	if err != nil {
		log.Fatalf("Couldn't load market data: %v", err)
	}

	store.v.SetMarketData(res.MarketData)
}

func (v *VegaClient) loadAccounts(config *Config, store *DataStore) {

	res, err := v.svc.ListAccounts(context.Background(), &apipb.ListAccountsRequest{Filter: &apipb.AccountFilter{PartyIds: []string{config.WalletPubkey}}})
	if err != nil {
		log.Fatalf("Couldn't load accounts: %v", err)
	}

	accounts := []*apipb.AccountBalance{}
	for _, a := range res.Accounts.Edges {
		accounts = append(accounts, a.Node)
	}

	store.v.SetAccounts(accounts)
}

func (v *VegaClient) loadOrders(config *Config, store *DataStore) {

	// res, err := v.svc.ListOrders(context.Background(), &apipb.ListOrdersRequest{PartyId: config.WalletPubkey, MarketId: config.VegaMarket, LiveOnly: true})
	liveOnly := true
	res, err := v.svc.ListOrders(context.Background(), &apipb.ListOrdersRequest{Filter: &apipb.OrderFilter{PartyIds: []string{config.WalletPubkey}, MarketIds: []string{config.VegaMarket}, LiveOnly: &liveOnly}})
	if err != nil {
		log.Fatalf("Couldn't load orders: %v", err)
	}

	orders := []*vegapb.Order{}
	for _, a := range res.Orders.Edges {
		orders = append(orders, a.Node)
	}

	store.v.SetOrders(orders)
}



func (v *VegaClient) loadPosition(config *Config, store *DataStore) {

	res, err := v.svc.ListPositions(context.Background(), &apipb.ListPositionsRequest{PartyId: config.WalletPubkey, MarketId: config.VegaMarket})
	if err != nil {
		log.Fatalf("Couldn't load positions: %v", err)
	}

	if len(res.Positions.Edges) > 1 {
		log.Fatalf("Invalid number of positions: %v", len(res.Positions.Edges))
	}

	if len(res.Positions.Edges) == 1 {
		store.v.SetPosition(res.Positions.Edges[0].Node)
	}
}

func (v *VegaClient) streamMarketData(config *Config, store *DataStore) {

	stream, err := v.svc.ObserveMarketsData(context.Background(), &apipb.ObserveMarketsDataRequest{MarketIds: []string{config.VegaMarket}})
	if err != nil {
		log.Fatalf("Failed to start Market Data stream: %v", err)
	}

	for {
		res, err := stream.Recv()
		if err != nil {
			log.Fatalf("Could not recieve on market data stream: %v", err)
		}

		fmt.Printf("Received market data on stream: %+v", res)
		
		for _, md := range res.MarketData {
			store.v.SetMarketData(md)
		}
	}
}

func (v *VegaClient) streamAccounts(config *Config, store *DataStore) {

	stream, err := v.svc.ObserveAccounts(context.Background(), &apipb.ObserveAccountsRequest{PartyId: config.VegaMarket})
	if err != nil {
		log.Fatalf("Failed to start accounts stream: %v", err)
	}

	for {
		res, err := stream.Recv()
		if err != nil {
			log.Fatalf("Could not recieve on accounts stream: %v", err)
		}

		fmt.Printf("Received accounts on stream: %+v", res)
		
		switch r := res.Response.(type) {
			case *apipb.ObserveAccountsResponse_Snapshot:
				store.v.SetAccounts(r.Snapshot.Accounts)
			case *apipb.ObserveAccountsResponse_Updates:
				store.v.SetAccounts(r.Updates.Accounts)
		}
	}
}

func (v *VegaClient) streamOrders(config *Config, store *DataStore) {

	stream, err := v.svc.ObserveOrders(context.Background(), &apipb.ObserveOrdersRequest{MarketIds: []string{config.VegaMarket}, PartyIds: []string{config.WalletPubkey}})
	if err != nil {
		log.Fatalf("Failed to start Orders stream: %v", err)
	}

	for {
		res, err := stream.Recv()
		if err != nil {
			log.Fatalf("Could not recieve on orders stream: %v", err)
		}

		fmt.Printf("Received orders on stream: %+v", res)

		switch r := res.Response.(type) {
			case *apipb.ObserveOrdersResponse_Snapshot:
				store.v.SetOrders(r.Snapshot.Orders)
			case *apipb.ObserveOrdersResponse_Updates:
				store.v.SetOrders(r.Updates.Orders)
		}
	}
}

func (v *VegaClient) streamPosition(config *Config, store *DataStore) {

	stream, err := v.svc.ObservePositions(context.Background(), &apipb.ObservePositionsRequest{MarketId: &config.VegaMarket, PartyId: &config.WalletPubkey})
	if err != nil {
		log.Fatalf("Failed to start positions stream: %v", err)
	}

	for {
		res, err := stream.Recv()
		if err != nil {
			log.Fatalf("Could not recieve on positions stream: %v", err)
		}

		fmt.Printf("Received position on stream: %+v", res)
		
		switch r := res.Response.(type) {
			case *apipb.ObservePositionsResponse_Snapshot:
				store.v.SetPosition(r.Snapshot.Positions[0])
			case *apipb.ObservePositionsResponse_Updates:
				store.v.SetPosition(r.Updates.Positions[0])
		}
	}
}