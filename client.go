package main

import (
	"context"
	// "encoding/json"
	"fmt"
	"log"
	// "reflect"
	// "strings"
	"sync"

	// "github.com/davecgh/go-spew/spew"
	apipb "code.vegaprotocol.io/vega/protos/data-node/api/v2"
	// apipb "vega-mm/protos/data-node/api/v2"
	vegapb "code.vegaprotocol.io/vega/protos/vega"
	// "github.com/gorilla/websocket"
	// "github.com/shopspring/decimal"
	"google.golang.org/grpc"
	// vegapb "vega-mm/protos/vega"
)

type VegaClient struct {
	agent         *agent
	grpcAddr      string
	grpcAddresses []string
	vegaMarkets   []string
	svc           apipb.TradingDataServiceClient
	reconnChan    chan struct{}
	reconnecting  bool
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

	fmt.Printf("Successes: %v\n", successes)
	fmt.Printf("Failures: %v\n", failures)

	fmt.Printf("Setting vegaClient grpcAddress to %v\n", successes[0])
	vegaClient.grpcAddr = successes[0]
}

func (agent *agent) RunVegaClientReconnectHandler() {

	for {
		select {
		case <-agent.vegaClient.reconnChan:
			log.Println("Recieved event on reconn channel")
			wg := &sync.WaitGroup{}
			wg.Add(1)
			go agent.StreamVegaData(wg)
			log.Println("Waiting for new vega data streams")
			wg.Wait()
			log.Println("Finished waiting for new vega data streams")
			agent.vegaClient.reconnecting = false
		}
	}
}

func (vegaClient *VegaClient) handleGrpcReconnect() {

	log.Println("Attempting reconnect.")

	if vegaClient.reconnecting {
		log.Println("Already reconnecting...")
		return
	}
	vegaClient.reconnecting = true
	vegaClient.reconnChan <- struct{}{}
}

func (agent *agent) StreamVegaData(wg *sync.WaitGroup) {

	// Test all available addresses
	agent.vegaClient.testGrpcAddresses()

	conn, err := grpc.Dial(agent.vegaClient.grpcAddr, grpc.WithInsecure())
	if err != nil {
		log.Printf("Could not open connection to datanode: %v\n", err)
		agent.vegaClient.handleGrpcReconnect()
		return
	}

	agent.vegaClient.svc = apipb.NewTradingDataServiceClient(conn)

	// marketIds := reflect.ValueOf(agent.strategies).MapKeys()

	// Load initial data
	agent.vegaClient.loadMarkets()
	agent.vegaClient.loadMarketData()
	agent.vegaClient.loadAccounts()
	agent.vegaClient.loadOrders()
	agent.vegaClient.loadPositions()
	agent.vegaClient.loadAssets()
	agent.vegaClient.loadLiquidityProvisions()

	// spew.Dump(d.s.v)

	// Start streams
	go agent.vegaClient.streamMarketData()
	go agent.vegaClient.streamAccounts()
	go agent.vegaClient.streamOrders()
	go agent.vegaClient.streamPositions()

	wg.Done()
}

func (v *VegaClient) loadMarkets() {

	for _, marketId := range v.vegaMarkets {

		res, err := v.svc.GetMarket(context.Background(), &apipb.GetMarketRequest{MarketId: marketId})
		if err != nil {
			log.Printf("Couldn't load Vega market: %v\n", err)
			v.handleGrpcReconnect()
			return
		}

		v.agent.strategies[marketId].vegaStore.SetMarket(res.Market)
	}
}

func (v *VegaClient) loadMarketData() {

	for _, marketId := range v.vegaMarkets {

		res, err := v.svc.GetLatestMarketData(context.Background(), &apipb.GetLatestMarketDataRequest{MarketId: marketId})
		if err != nil {
			log.Printf("Couldn't load market data: %v\n", err)
			v.handleGrpcReconnect()
			return
		}

		v.agent.strategies[marketId].vegaStore.SetMarketData(res.MarketData)
	}
}

func (v *VegaClient) loadAccounts() {

	res, err := v.svc.ListAccounts(context.Background(), &apipb.ListAccountsRequest{Filter: &apipb.AccountFilter{PartyIds: []string{v.agent.pubkey}}})
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
		v.agent.strategies[marketId].vegaStore.SetAccounts(accounts)
	}
}

func (v *VegaClient) loadOrders() {

	for _, marketId := range v.vegaMarkets {

		// res, err := v.svc.ListOrders(context.Background(), &apipb.ListOrdersRequest{PartyId: v.agent.pubkey, MarketId: config.VegaMarket, LiveOnly: true})
		liveOnly := true
		res, err := v.svc.ListOrders(context.Background(), &apipb.ListOrdersRequest{Filter: &apipb.OrderFilter{PartyIds: []string{v.agent.pubkey}, MarketIds: []string{marketId}, LiveOnly: &liveOnly}})
		if err != nil {
			log.Printf("Couldn't load orders: %v\n", err)
			v.handleGrpcReconnect()
			return
		}

		orders := []*vegapb.Order{}
		for _, a := range res.Orders.Edges {
			orders = append(orders, a.Node)
		}

		v.agent.strategies[marketId].vegaStore.SetOrders(orders)

	}
}

func (v *VegaClient) loadPositions() {

	for _, marketId := range v.vegaMarkets {

		res, err := v.svc.ListPositions(context.Background(), &apipb.ListPositionsRequest{PartyId: v.agent.pubkey, MarketId: marketId})
		if err != nil {
			log.Printf("Couldn't load positions: %v\n", err)
			v.handleGrpcReconnect()
			return
		}

		if len(res.Positions.Edges) > 1 {
			log.Fatalf("Invalid number of positions: %v", len(res.Positions.Edges))
		}

		if len(res.Positions.Edges) == 1 {
			v.agent.strategies[marketId].vegaStore.SetPosition(res.Positions.Edges[0].Node)
		}

	}
}

func (v *VegaClient) loadAssets() {

	for _, marketId := range v.vegaMarkets {

		res, err := v.svc.ListAssets(context.Background(), &apipb.ListAssetsRequest{})
		if err != nil {
			log.Printf("Couldn't load assets: %v\n", err)
			v.handleGrpcReconnect()
			return
		}

		for _, a := range res.Assets.Edges {
			v.agent.strategies[marketId].vegaStore.SetAsset(a.Node)
		}
	}
}

func (v *VegaClient) loadLiquidityProvisions() {

	for _, marketId := range v.vegaMarkets {

		res, err := v.svc.ListLiquidityProvisions(context.Background(), &apipb.ListLiquidityProvisionsRequest{MarketId: &marketId, PartyId: &v.agent.pubkey})
		if err != nil {
			log.Printf("Couldn't load liquidity provisions: %v\n", err)
			v.handleGrpcReconnect()
			return
		}

		for _, a := range res.LiquidityProvisions.Edges {
			v.agent.strategies[marketId].vegaStore.SetLiquidityProvision(a.Node)
		}

		// log.Printf("Liquidity Provisions for market: %v: %v", marketId, res.LiquidityProvisions.Edges)
	}
}

func (v *VegaClient) streamMarketData() {

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
					v.agent.strategies[marketId].vegaStore.SetMarketData(md)
				}
			}
		}(marketId, stream)

	}
}

func (v *VegaClient) streamAccounts() {

	stream, err := v.svc.ObserveAccounts(context.Background(), &apipb.ObserveAccountsRequest{PartyId: v.agent.pubkey})
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
				v.agent.strategies[marketId].vegaStore.SetAccounts(r.Snapshot.Accounts)
			case *apipb.ObserveAccountsResponse_Updates:
				v.agent.strategies[marketId].vegaStore.SetAccounts(r.Updates.Accounts)
			}
		}
	}
}

func (v *VegaClient) streamOrders() {

	for _, marketId := range v.vegaMarkets {

		stream, err := v.svc.ObserveOrders(context.Background(), &apipb.ObserveOrdersRequest{MarketIds: []string{marketId}, PartyIds: []string{v.agent.pubkey}})
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
					v.agent.strategies[marketId].vegaStore.SetOrders(r.Snapshot.Orders)
				case *apipb.ObserveOrdersResponse_Updates:
					v.agent.strategies[marketId].vegaStore.SetOrders(r.Updates.Orders)
				}
			}
		}(marketId, stream)
	}
}

func (v *VegaClient) streamPositions() {

	for _, marketId := range v.vegaMarkets {

		stream, err := v.svc.ObservePositions(context.Background(), &apipb.ObservePositionsRequest{MarketId: &marketId, PartyId: &v.agent.pubkey})
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
					v.agent.strategies[marketId].vegaStore.SetPosition(r.Snapshot.Positions[0])
				case *apipb.ObservePositionsResponse_Updates:
					v.agent.strategies[marketId].vegaStore.SetPosition(r.Updates.Positions[0])
				}
			}
		}(marketId, stream)
	}
}
