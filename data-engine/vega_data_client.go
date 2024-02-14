package data

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"
	"vega-mm/stores"

	apipb "code.vegaprotocol.io/vega/protos/data-node/api/v2"
	"golang.org/x/exp/maps"

	vegapb "code.vegaprotocol.io/vega/protos/vega"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type pubkey string
type vegaMarketId string
type binanceTicker string

type VegaDataClient struct {
	de            *DataEngine
	grpcAddr      string
	grpcAddresses []string
	vegaMarkets   []string
	agentPubkeys  []string

	stores         map[string]map[string]*stores.VegaStore // map[marketId]map[pubkey]*stores.VegaStore
	storesByMarket map[string][]*stores.VegaStore
	storesByPubkey map[string][]*stores.VegaStore

	storesSlice []*stores.VegaStore

	svc          apipb.TradingDataServiceClient
	reconnChan   chan struct{}
	reconnecting bool
}

func NewVegaDataClient(grpcAddrs []string) *VegaDataClient {
	return &VegaDataClient{
		grpcAddresses: grpcAddrs,
		reconnChan:    make(chan struct{}),
	}
}

func (v *VegaDataClient) Init(dataStores []*stores.VegaStore) *VegaDataClient {

	v.stores = map[string]map[string]*stores.VegaStore{}
	v.storesByMarket = map[string][]*stores.VegaStore{}
	v.storesByPubkey = map[string][]*stores.VegaStore{}
	v.storesSlice = dataStores

	for _, store := range v.storesSlice {
		marketId, pubkey := store.GetMarketId(), store.GetAgentPubKey()

		v.storesByMarket[marketId] = append(v.storesByMarket[marketId], store)
		v.storesByPubkey[pubkey] = append(v.storesByPubkey[pubkey], store)
		_, ok := v.stores[marketId]
		if !ok {
			v.stores[marketId] = map[string]*stores.VegaStore{}
		}
		v.stores[marketId][pubkey] = store
	}

	v.vegaMarkets = maps.Keys(v.storesByMarket)
	v.agentPubkeys = maps.Keys(v.storesByPubkey)

	ok := v.testGrpcAddresses()
	if !ok {
		log.Fatalf("Failed to connect to a datanode.\n")
	}

	return v
}

func (v *VegaDataClient) Start(wg *sync.WaitGroup) {
	go v.RunVegaClientReconnectHandler()
	go v.StreamVegaData(wg)
}

func (vegaClient *VegaDataClient) testGrpcAddresses() (ok bool) {

	log.Printf("Testing datanode gRPC addresses.\n")

	type successfulTest struct {
		addr      string
		latencyMs int64
	}

	// successes := []string{}
	successes := []successfulTest{}
	failures := []string{}

	for _, addr := range vegaClient.grpcAddresses {

		startTimeMs := time.Now().UnixMilli()

		conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
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

		latency := time.Now().UnixMilli() - startTimeMs

		successes = append(successes, successfulTest{
			addr:      addr,
			latencyMs: latency,
		})
		conn.Close()

	}

	fmt.Printf("Successes: %+v\n", successes)
	fmt.Printf("Failures: %v\n", failures)
	sort.Slice(successes, func(i, j int) bool {
		return successes[i].latencyMs < successes[j].latencyMs
	})

	if len(successes) == 0 {
		ok = false
		vegaClient.grpcAddr = ""
		return
	} else {
		ok = true
	}

	fmt.Printf("Lowest latency grpc address was: %v with %vms latency\n", successes[0].addr, successes[0].latencyMs)
	fmt.Printf("Setting vegaClient grpc address to %v\n", successes[0])
	vegaClient.grpcAddr = successes[0].addr
	return
}

// This reconnect logic has some rare edge cases where a datanode fails after it has been tested successfully
// but before all the trading data is fully loaded from it. Refactor this to handle these edge cases.
func (vegaClient *VegaDataClient) RunVegaClientReconnectHandler() {

	for {
		select {
		case <-vegaClient.reconnChan:
			log.Println("Recieved event on reconn channel")

			vegaClient.grpcAddr = ""
			ok := false
			for !ok {
				ok = vegaClient.testGrpcAddresses()
				if !ok {
					n := 30
					log.Printf("Failed to reconnect to a data node. Waiting %v seconds before retry", n)
					time.Sleep(time.Second * 30)
				}
			}

			vegaClient.reconnecting = false

			wg := &sync.WaitGroup{}
			wg.Add(1)
			go vegaClient.StreamVegaData(wg)
			log.Println("Waiting for new vega data streams")
			wg.Wait()
			log.Println("Finished reconnecting to a data node.")
		}
	}
}

func (vegaClient *VegaDataClient) handleGrpcReconnect() {

	log.Println("Attempting reconnect.")

	if vegaClient.reconnecting {
		log.Println("Already reconnecting...")
		return
	}
	vegaClient.reconnecting = true
	vegaClient.reconnChan <- struct{}{}
}

func (vegaClient *VegaDataClient) StreamVegaData(wg *sync.WaitGroup) {

	// Test all available addresses
	ok := vegaClient.testGrpcAddresses()
	if !ok { // Test failed for all addrs. If we don't have other reference price we should stop quoting.
		log.Fatal("No vega datanodes are reachable... Exiting.\n")
	}

	conn, err := grpc.Dial(vegaClient.grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Could not open connection to datanode: %v\n", err)
		// log.Printf("Could not open connection to datanode: %v\n", err)
		// vegaClient.handleGrpcReconnect()
		return
	}

	vegaClient.svc = apipb.NewTradingDataServiceClient(conn)

	// Load initial data
	vegaClient.loadMarkets()
	vegaClient.loadMarketData()
	vegaClient.loadAccounts()
	// vegaClient.loadOrders()
	vegaClient.loadPositions()
	vegaClient.loadAssets()
	vegaClient.loadLiquidityProvisions()
	vegaClient.loadStakeToCcyVolume()

	// Start streams
	go vegaClient.streamMarketData()
	go vegaClient.streamAccounts()
	go vegaClient.streamOrders()
	go vegaClient.streamPositions()

	wg.Done()
}

func (v *VegaDataClient) loadMarkets() {

	for _, marketId := range v.vegaMarkets {

		res, err := v.svc.GetMarket(context.Background(), &apipb.GetMarketRequest{MarketId: marketId})
		if err != nil {
			log.Printf("Couldn't load Vega market: %v\n", err)
			v.handleGrpcReconnect()
			return
		}

		for _, store := range v.stores[marketId] {
			store.SetMarket(res.Market)
		}

	}
}

func (v *VegaDataClient) loadMarketData() {

	for _, marketId := range v.vegaMarkets {

		res, err := v.svc.GetLatestMarketData(context.Background(), &apipb.GetLatestMarketDataRequest{MarketId: marketId})
		if err != nil {
			log.Printf("Couldn't load market data: %v\n", err)
			v.handleGrpcReconnect()
			return
		}

		for _, store := range v.stores[marketId] {
			store.SetMarketData(res.MarketData)
		}

	}
}

func (v *VegaDataClient) loadAccounts() {

	for _, pubkey := range v.agentPubkeys {
		res, err := v.svc.ListAccounts(context.Background(), &apipb.ListAccountsRequest{Filter: &apipb.AccountFilter{PartyIds: []string{pubkey}}})
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
			for pk, store := range v.stores[marketId] {
				if string(pk) == pubkey {
					store.SetAccounts(accounts)
				}
			}
		}

	}
}

// func (v *VegaDataClient) loadOrders() {

// 	for _, marketId := range v.vegaMarkets {

// 		// res, err := v.svc.ListOrders(context.Background(), &apipb.ListOrdersRequest{PartyId: v.agent.pubkey, MarketId: config.VegaMarket, LiveOnly: true})
// 		liveOnly := true
// 		res, err := v.svc.ListOrders(context.Background(), &apipb.ListOrdersRequest{Filter: &apipb.OrderFilter{PartyIds: []string{v.agent.pubkey}, MarketIds: []string{marketId}, LiveOnly: &liveOnly}})
// 		if err != nil {
// 			log.Printf("Couldn't load orders: %v\n", err)
// 			v.handleGrpcReconnect()
// 			return
// 		}

// 		orders := []*vegapb.Order{}
// 		for _, a := range res.Orders.Edges {
// 			orders = append(orders, a.Node)
// 		}

// 		v.agent.strategies[marketId].vegaStore.SetOrders(orders)

// 	}
// }

func (v *VegaDataClient) loadPositions() {

	reqFilter := &apipb.PositionsFilter{PartyIds: v.agentPubkeys, MarketIds: v.vegaMarkets}
	req := &apipb.ListAllPositionsRequest{Filter: reqFilter}
	res, err := v.svc.ListAllPositions(context.Background(), req)
	if err != nil {
		log.Printf("Couldn't load positions: %v\n", err)
		v.handleGrpcReconnect()
		return
	}

	for _, edge := range res.Positions.Edges {
		if _, ok := v.stores[edge.Node.GetMarketId()]; !ok {
			continue
		}
		if _, ok := v.stores[edge.Node.GetMarketId()][edge.Node.GetPartyId()]; !ok {
			continue
		}
		v.stores[edge.Node.GetMarketId()][edge.Node.GetPartyId()].SetPosition(edge.Node)
	}

}

func (v *VegaDataClient) loadAssets() {

	res, err := v.svc.ListAssets(context.Background(), &apipb.ListAssetsRequest{})
	if err != nil {
		log.Printf("Couldn't load assets: %v\n", err)
		v.handleGrpcReconnect()
		return
	}

	for _, a := range res.Assets.Edges {
		for _, store := range v.storesSlice {
			store.SetAsset(a.Node)
		}
	}
}

func (v *VegaDataClient) loadLiquidityProvisions() {

	for _, store := range v.storesSlice {

		marketId := store.GetMarketId()
		partyId := store.GetAgentPubKey()
		req := &apipb.ListAllLiquidityProvisionsRequest{MarketId: &marketId, PartyId: &partyId}
		res, err := v.svc.ListAllLiquidityProvisions(context.Background(), req)
		if err != nil {
			log.Printf("Couldn't load liquidity provisions: %v\n", err)
			v.handleGrpcReconnect()
			return
		}

		for _, a := range res.LiquidityProvisions.Edges {
			v.stores[marketId][partyId].SetLiquidityProvision(a.Node.Pending)
		}

		// log.Printf("Liquidity Provisions for market: %v: %v", marketId, res.LiquidityProvisions.Edges)
	}
}

func (v *VegaDataClient) loadStakeToCcyVolume() {

	res, err := v.svc.GetNetworkParameter(context.Background(), &apipb.GetNetworkParameterRequest{Key: "market.liquidity.stakeToCcyVolume"})
	if err != nil {
		log.Printf("Could not get stakeToCcyVolume net param: %v\n", err)
		v.handleGrpcReconnect()
		return
	}

	netParam := res.GetNetworkParameter()

	for _, store := range v.storesSlice {
		store.SetStakeToCcyVolume(netParam)
	}

}

// Func for loading initial network params
func (v *VegaDataClient) loadNetworkParams() {

}

// Will this stream just timeout all the time due to infrequent messages?
// Test it to find out.res, err := v.svc.ListNetworkParameters()
// Better solution might be to periodically get active governance proposals and check for net param updates.
func (v *VegaDataClient) streamNetworkParams() {

	// Stream governance and check for param changes

}

func (v *VegaDataClient) streamMarketData() {

	stream, err := v.svc.ObserveMarketsData(context.Background(), &apipb.ObserveMarketsDataRequest{MarketIds: v.vegaMarkets})
	if err != nil {
		log.Printf("Failed to start Market Data stream: %v\n", err)
		v.handleGrpcReconnect()
		return
	}

	go func(stream apipb.TradingDataService_ObserveMarketsDataClient) {
		for {
			res, err := stream.Recv()
			if err != nil {
				log.Printf("Could not recieve on market data stream: %v\n", err)
				v.handleGrpcReconnect()
				return
			}

			// fmt.Printf("Received market data on stream: %+v", res)

			for _, md := range res.MarketData {
				for _, store := range v.storesSlice {
					if store.GetMarketId() == md.GetMarket() {
						store.SetMarketData(md)
					}
				}
			}
		}
	}(stream)

}

func (v *VegaDataClient) streamAccounts() {

	for _, pubkey := range v.agentPubkeys {

		stream, err := v.svc.ObserveAccounts(context.Background(), &apipb.ObserveAccountsRequest{PartyId: pubkey})
		if err != nil {
			log.Printf("Failed to start accounts stream: %v\n", err)
			v.handleGrpcReconnect()
			return
		}

		go func(pubkey string, stream apipb.TradingDataService_ObserveAccountsClient) {
			for {
				res, err := stream.Recv()
				if err != nil {
					log.Printf("Could not recieve on accounts stream: %v\n", err)
					v.handleGrpcReconnect()
					return
				}

				// fmt.Printf("Received accounts on stream: %+v", res)

				for _, store := range v.storesByPubkey[pubkey] {
					switch r := res.Response.(type) {
					case *apipb.ObserveAccountsResponse_Snapshot:
						store.SetAccounts(r.Snapshot.Accounts)
					case *apipb.ObserveAccountsResponse_Updates:
						store.SetAccounts(r.Updates.Accounts)
					}
				}

			}
		}(pubkey, stream)

	}
}

func (v *VegaDataClient) streamOrders() {

	stream, err := v.svc.ObserveOrders(context.Background(), &apipb.ObserveOrdersRequest{MarketIds: v.vegaMarkets, PartyIds: v.agentPubkeys})
	if err != nil {
		log.Printf("Failed to start Orders stream: %v\n", err)
		v.handleGrpcReconnect()
		return
	}

	for _, store := range v.storesSlice {
		// Empty store before we start stream in case of reconnect with stale orders
		store.ClearOrders()
	}

	orderMap := map[string]map[string][]*vegapb.Order{}
	for _, marketId := range v.vegaMarkets {
		orderMap[marketId] = map[string][]*vegapb.Order{}
	}

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
			for _, order := range r.Snapshot.Orders {
				orderMap[order.MarketId][order.PartyId] = append(orderMap[order.MarketId][order.PartyId], order)
			}
			for _, store := range v.storesSlice {
				store.SetOrders(orderMap[store.GetMarketId()][store.GetAgentPubKey()])
				orderMap[store.GetMarketId()][store.GetAgentPubKey()] = nil
			}
		case *apipb.ObserveOrdersResponse_Updates:
			for _, order := range r.Updates.Orders {
				orderMap[order.MarketId][order.PartyId] = append(orderMap[order.MarketId][order.PartyId], order)
			}
			for _, store := range v.storesSlice {
				store.SetOrders(orderMap[store.GetMarketId()][store.GetAgentPubKey()])
				orderMap[store.GetMarketId()][store.GetAgentPubKey()] = nil
			}
		}
	}

}

func (v *VegaDataClient) streamPositions() {

	for _, pubkey := range v.agentPubkeys {

		stream, err := v.svc.ObservePositions(context.Background(), &apipb.ObservePositionsRequest{PartyId: &pubkey})
		if err != nil {
			log.Printf("Failed to start positions stream: %v\n", err)
			v.handleGrpcReconnect()
			return
		}

		go func(pubkey string, stream apipb.TradingDataService_ObservePositionsClient) {
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
					for _, pos := range r.Snapshot.Positions {
						if _, ok := v.stores[pos.GetMarketId()]; !ok {
							continue
						}
						if _, ok := v.stores[pos.GetMarketId()][pubkey]; !ok {
							continue
						}
						v.stores[pos.GetMarketId()][pubkey].SetPosition(pos)
					}
				case *apipb.ObservePositionsResponse_Updates:
					for _, pos := range r.Updates.Positions {
						if _, ok := v.stores[pos.GetMarketId()]; !ok {
							continue
						}
						if _, ok := v.stores[pos.GetMarketId()][pubkey]; !ok {
							continue
						}
						v.stores[pos.GetMarketId()][pubkey].SetPosition(pos)
					}
				}
			}
		}(pubkey, stream)
	}
}
