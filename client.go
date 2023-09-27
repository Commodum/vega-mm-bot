package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	// "github.com/davecgh/go-spew/spew"
	apipb "code.vegaprotocol.io/vega/protos/data-node/api/v2"
	vegapb "code.vegaprotocol.io/vega/protos/vega"
	"github.com/gorilla/websocket"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
)

type VegaClient struct {
	grpcAddresses	[]string
	grpcAddr    string
	vegaMarkets []string
	svc         apipb.TradingDataServiceClient
	reconnChan	chan struct{}
	reconnecting bool
}

type BinanceClient struct {
	wsAddr         string
	binanceMarkets []string
	conn           *websocket.Conn
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
			wsAddr:         config.BinanceWsAddr,
			binanceMarkets: strings.Split(config.BinanceMarkets, ","),
		},
		v: &VegaClient{
			grpcAddr:    config.VegaGrpcAddr,
			grpcAddresses: strings.Split(config.VegaGrpcAddresses, ","),
			vegaMarkets: strings.Split(config.VegaMarkets, ","),
			reconnChan: make(chan struct{}),
			reconnecting: false,
		},
		c: config,
		s: store,
	}
}

func (d *DataClient) streamBinanceData(wg *sync.WaitGroup) {

	markets := strings.Split(d.c.BinanceMarkets, ",")

	for _, mkt := range markets {
		d.s.b[mkt] = newBinanceStore(mkt)
	}

	conn, _, err := websocket.DefaultDialer.Dial(d.b.wsAddr, nil)
	if err != nil {
		log.Fatal("Dial Error: ", err)
	}
	d.b.conn = conn

	req := struct {
		Id     uint     `json:"id"`
		Method string   `json:"method"`
		Params []string `json:"params"`
	}{
		Id:     1,
		Method: "SUBSCRIBE",
		Params: []string{
			fmt.Sprintf("%s@ticker", strings.ToLower(markets[0])),
			fmt.Sprintf("%s@ticker", strings.ToLower(markets[1])),
			fmt.Sprintf("%s@ticker", strings.ToLower(markets[2])),
		},
	}

	out, _ := json.Marshal(req)

	if err = conn.WriteMessage(websocket.TextMessage, out); err != nil {
		log.Fatalf("Could not write on Binance Websocket: %v", err)
	}

	_, msg, err := conn.ReadMessage()
	if err != nil {
		log.Fatalf("Could not read from Binance websocket: %v", err)
	}
	log.Printf("Message received: %v", string(msg))

	// wg.Done()

	response := struct {
		Ticker   string          `json:"s"`
		Type     string          `json:"e"`
		AskPrice decimal.Decimal `json:"a"`
		BidPrice decimal.Decimal `json:"b"`
		// Dummy properties to prevent unmarshalling capitalised property
		// names into lowercase properties.
		Timestamp uint64 `json:"E"`
		NotA      string `json:"A"`
		NotB      string `json:"B"`
	}{}

	go func(conn *websocket.Conn) {
		defer conn.Close()
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				// fmt.Println(err)
				// log.Printf("Could not read message from websocket... Reconnecting...")
				// conn.Close()

				// conn, _, err = websocket.DefaultDialer.Dial(d.b.wsAddr, nil)
				// if err != nil {
				// 	log.Fatal("Dial Error: ", err)
				// }
				// log.Printf("Reconnected to Binance WS")
				log.Fatalf("Could not read message from websocket: %v", err)
			}
			// fmt.Printf("Message received: %v\n", string(msg))

			err = json.Unmarshal(msg, &response)
			if err != nil {
				log.Fatalf("Could not unmarshal websocket response: %v - %v", err, msg)
			}

			// fmt.Println(string(msg))
			// fmt.Printf("Response Data: %+v\n", response)

			// Check content of response.

			// Set value in store
			d.s.b[strings.Clone(response.Ticker)].Set(response.BidPrice.Copy(), response.AskPrice.Copy())
		}
	}(conn)

	for {
		doneCount := 0
		for _, market := range markets {
			bid, _ := d.s.b[market].Get()
			if bid.IsZero() {
				continue
			} else {
				doneCount++
			}
		}
		if doneCount == len(markets) {
			wg.Done()
			break
		}
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

func (vegaClient *VegaClient) testGrpcAddresses() {

	successes := []string{}
	failures := []string{}

	for _, addr := range vegaClient.grpcAddresses {

		conn, err := grpc.Dial(addr, grpc.WithInsecure())
		if err != nil {
			log.Printf("Could not open connection to datanode (%v): %v", addr, err)
			failures = append(failures, addr)
			continue
		}

		grpcClient := apipb.NewTradingDataServiceClient(conn)

		_, err = grpcClient.GetMarket(context.Background(), &apipb.GetMarketRequest{MarketId: vegaClient.vegaMarkets[0]})
		if err != nil {
			log.Printf("Could not get market from url: %v. Error: %v", addr, err)
			failures = append(failures, addr)
			conn.Close()
			continue
		}

		successes = append(successes, addr)
		conn.Close()

	}

	fmt.Printf("Successes: %v", successes)
	fmt.Printf("Failures: %v", failures)

	fmt.Printf("Setting vegaClient grpcAddress to %v", successes[0])
	vegaClient.grpcAddr = successes[0]
}

func (dataClient *DataClient) runVegaClientReconnectHandler() {
	
	for {
		select {
		case <- dataClient.v.reconnChan:
			wg := &sync.WaitGroup{}
			wg.Add(1)
			go dataClient.streamVegaData(wg)
			wg.Wait()
			dataClient.v.reconnecting = false
		}
	}

}

func (vegaClient *VegaClient) handleGrpcReconnect() {

	if vegaClient.reconnecting { return }
	vegaClient.reconnecting = true
	vegaClient.reconnChan <- struct{}{}

}

func (d *DataClient) streamVegaData(wg *sync.WaitGroup) {

	// Test all available addresses
	d.v.testGrpcAddresses();

	conn, err := grpc.Dial(d.v.grpcAddr, grpc.WithInsecure())
	if err != nil {
		log.Printf("Could not open connection to datanode: %v\n", err)
		d.v.handleGrpcReconnect()
		return
	}

	d.v.svc = apipb.NewTradingDataServiceClient(conn)

	// Load initial data
	d.v.loadMarketIds(d.s)
	d.v.loadMarkets(d.s)
	d.v.loadMarketData(d.s)
	d.v.loadAccounts(d.c, d.s)
	d.v.loadOrders(d.c, d.s)
	d.v.loadPositions(d.c, d.s)
	d.v.loadAssets(d.s)
	d.v.loadLiquidityProvisions(d.c, d.s)

	// spew.Dump(d.s.v)

	// Start streams
	go d.v.streamMarketData(d.c, d.s)
	go d.v.streamAccounts(d.c, d.s)
	go d.v.streamOrders(d.c, d.s)
	go d.v.streamPositions(d.c, d.s)

	wg.Done()
}

func (v *VegaClient) loadMarketIds(store *DataStore) {
	for _, marketId := range v.vegaMarkets {
		store.v[marketId] = newVegaStore(marketId)
	}
}

func (v *VegaClient) loadMarkets(store *DataStore) {

	for _, marketId := range v.vegaMarkets {

		res, err := v.svc.GetMarket(context.Background(), &apipb.GetMarketRequest{MarketId: marketId})
		if err != nil {
			log.Printf("Couldn't load Vega market: %v\n", err)
			v.handleGrpcReconnect()
			return
		}

		store.v[marketId].SetMarket(res.Market)
	}
}

func (v *VegaClient) loadMarketData(store *DataStore) {

	for _, marketId := range v.vegaMarkets {

		res, err := v.svc.GetLatestMarketData(context.Background(), &apipb.GetLatestMarketDataRequest{MarketId: marketId})
		if err != nil {
			log.Printf("Couldn't load market data: %v\n", err)
			v.handleGrpcReconnect()
			return
		}

		store.v[marketId].SetMarketData(res.MarketData)
	}
}

func (v *VegaClient) loadAccounts(config *Config, store *DataStore) {

	res, err := v.svc.ListAccounts(context.Background(), &apipb.ListAccountsRequest{Filter: &apipb.AccountFilter{PartyIds: []string{config.WalletPubkey}}})
	if err != nil {
		log.Printf("Couldn't load accounts: %v\n", err)
		v.handleGrpcReconnect()
		return
	}

	accounts := []*apipb.AccountBalance{}
	for _, a := range res.Accounts.Edges {
		accounts = append(accounts, a.Node)
	}

	for _, marketId := range v.vegaMarkets {
		store.v[marketId].SetAccounts(accounts)
	}
}

func (v *VegaClient) loadOrders(config *Config, store *DataStore) {

	for _, marketId := range v.vegaMarkets {

		// res, err := v.svc.ListOrders(context.Background(), &apipb.ListOrdersRequest{PartyId: config.WalletPubkey, MarketId: config.VegaMarket, LiveOnly: true})
		liveOnly := true
		res, err := v.svc.ListOrders(context.Background(), &apipb.ListOrdersRequest{Filter: &apipb.OrderFilter{PartyIds: []string{config.WalletPubkey}, MarketIds: []string{marketId}, LiveOnly: &liveOnly}})
		if err != nil {
			log.Printf("Couldn't load orders: %v\n", err)
			v.handleGrpcReconnect()
			return
		}

		orders := []*vegapb.Order{}
		for _, a := range res.Orders.Edges {
			orders = append(orders, a.Node)
		}

		store.v[marketId].SetOrders(orders)

	}
}

func (v *VegaClient) loadPositions(config *Config, store *DataStore) {

	for _, marketId := range v.vegaMarkets {

		res, err := v.svc.ListPositions(context.Background(), &apipb.ListPositionsRequest{PartyId: config.WalletPubkey, MarketId: marketId})
		if err != nil {
			log.Printf("Couldn't load positions: %v\n", err)
			v.handleGrpcReconnect()
			return
		}

		if len(res.Positions.Edges) > 1 {
			log.Fatalf("Invalid number of positions: %v", len(res.Positions.Edges))
		}

		if len(res.Positions.Edges) == 1 {
			store.v[marketId].SetPosition(res.Positions.Edges[0].Node)
		}

	}
}

func (v *VegaClient) loadAssets(store *DataStore) {

	for _, marketId := range v.vegaMarkets {

		res, err := v.svc.ListAssets(context.Background(), &apipb.ListAssetsRequest{})
		if err != nil {
			log.Printf("Couldn't load assets: %v\n", err)
			v.handleGrpcReconnect()
			return
		}

		for _, a := range res.Assets.Edges {
			store.v[marketId].SetAsset(a.Node)
		}
	}
}

func (v *VegaClient) loadLiquidityProvisions(config *Config, store *DataStore) {

	for _, marketId := range v.vegaMarkets {

		res, err := v.svc.ListLiquidityProvisions(context.Background(), &apipb.ListLiquidityProvisionsRequest{MarketId: &marketId, PartyId: &config.WalletPubkey})
		if err != nil {
			log.Printf("Couldn't load liquidity provisions: %v\n", err)
			v.handleGrpcReconnect()
			return
		}

		for _, a := range res.LiquidityProvisions.Edges {
			store.v[marketId].SetLiquidityProvision(a.Node)
		}

		log.Printf("Liquidity Provisions for market: %v: %v", marketId, res.LiquidityProvisions.Edges)
	}
}

func (v *VegaClient) streamMarketData(config *Config, store *DataStore) {

	for _, marketId := range v.vegaMarkets {

		stream, err := v.svc.ObserveMarketsData(context.Background(), &apipb.ObserveMarketsDataRequest{MarketIds: []string{marketId}})
		if err != nil {
			log.Printf("Failed to start Market Data stream: %v\n", err)
			v.handleGrpcReconnect()
			return
		}

		go func(marketId string, stream apipb.TradingDataService_ObserveMarketsDataClient) {
			for {
				res, err := stream.Recv()
				if err != nil {
					log.Printf("Could not recieve on market data stream: %v\n", err)
					v.handleGrpcReconnect()
					return
				}

				// fmt.Printf("Received market data on stream: %+v", res)

				for _, md := range res.MarketData {
					store.v[marketId].SetMarketData(md)
				}
			}
		}(marketId, stream)

	}
}

func (v *VegaClient) streamAccounts(config *Config, store *DataStore) {

	stream, err := v.svc.ObserveAccounts(context.Background(), &apipb.ObserveAccountsRequest{PartyId: config.WalletPubkey})
	if err != nil {
		log.Printf("Failed to start accounts stream: %v\n", err)
		v.handleGrpcReconnect()
		return
	}

	for {
		res, err := stream.Recv()
		if err != nil {
			log.Printf("Could not recieve on accounts stream: %v\n", err)
			v.handleGrpcReconnect()
			return
		}

		// fmt.Printf("Received accounts on stream: %+v", res)

		for _, marketId := range v.vegaMarkets {
			switch r := res.Response.(type) {
			case *apipb.ObserveAccountsResponse_Snapshot:
				store.v[marketId].SetAccounts(r.Snapshot.Accounts)
			case *apipb.ObserveAccountsResponse_Updates:
				store.v[marketId].SetAccounts(r.Updates.Accounts)
			}
		}
	}
}

func (v *VegaClient) streamOrders(config *Config, store *DataStore) {

	for _, marketId := range v.vegaMarkets {

		stream, err := v.svc.ObserveOrders(context.Background(), &apipb.ObserveOrdersRequest{MarketIds: []string{marketId}, PartyIds: []string{config.WalletPubkey}})
		if err != nil {
			log.Printf("Failed to start Orders stream: %v\n", err)
			v.handleGrpcReconnect()
			return
		}

		go func(marketId string, stream apipb.TradingDataService_ObserveOrdersClient) {
			for {
				res, err := stream.Recv()
				if err != nil {
					log.Printf("Could not recieve on orders stream: %v\n", err)
					v.handleGrpcReconnect()
					return
				}

				// fmt.Printf("Received orders on stream: %+v", res)

				switch r := res.Response.(type) {
				case *apipb.ObserveOrdersResponse_Snapshot:
					store.v[marketId].SetOrders(r.Snapshot.Orders)
				case *apipb.ObserveOrdersResponse_Updates:
					store.v[marketId].SetOrders(r.Updates.Orders)
				}
			}
		}(marketId, stream)
	}
}

func (v *VegaClient) streamPositions(config *Config, store *DataStore) {

	for _, marketId := range v.vegaMarkets {

		stream, err := v.svc.ObservePositions(context.Background(), &apipb.ObservePositionsRequest{MarketId: &marketId, PartyId: &config.WalletPubkey})
		if err != nil {
			log.Printf("Failed to start positions stream: %v\n", err)
			v.handleGrpcReconnect()
			return
		}

		go func(marketId string, stream apipb.TradingDataService_ObservePositionsClient) {
			for {
				res, err := stream.Recv()
				if err != nil {
					log.Printf("Could not recieve on positions stream: %v\n", err)
					v.handleGrpcReconnect()
					return
				}

				// fmt.Printf("Received position on stream: %+v", res)

				switch r := res.Response.(type) {
				case *apipb.ObservePositionsResponse_Snapshot:
					store.v[marketId].SetPosition(r.Snapshot.Positions[0])
				case *apipb.ObservePositionsResponse_Updates:
					store.v[marketId].SetPosition(r.Updates.Positions[0])
				}
			}
		}(marketId, stream)
	}
}
