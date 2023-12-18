package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	apipb "code.vegaprotocol.io/vega/protos/data-node/api/v2"
	"github.com/gorilla/websocket"
	"github.com/shopspring/decimal"

	vegapb "code.vegaprotocol.io/vega/protos/vega"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type VegaCoreClient struct {
}

type VegaDataClient struct {
	agent         *agent
	de            *DataEngine
	grpcAddr      string
	grpcAddresses []string
	vegaMarkets   []string
	svc           apipb.TradingDataServiceClient
	reconnChan    chan struct{}
	reconnecting  bool
}

type BinanceClient struct {
	agent        *agent
	de           *DataEngine
	wsAddr       string
	markets      map[string]string
	reconnChan   chan struct{}
	reconnecting bool
}

func newBinanceClient(agent *agent, wsAddr string) *BinanceClient {
	return &BinanceClient{
		agent:      agent,
		wsAddr:     wsAddr,
		markets:    map[string]string{},
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
			for _, strat := range b.agent.strategies {
				strat.binanceStore.isStale = false
			}
		}
	}

}

func (b *BinanceClient) StreamBinanceData() {

	delayReconnect := func() {
		for _, strat := range b.agent.strategies {
			strat.binanceStore.isStale = true // Mark binance data as stale as we might not be able to reconnect for a while
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

	reqParams := []string{}
	// for binanceMarket := range b.markets {
	// 	reqParams = append(reqParams, fmt.Sprintf("%s@ticker", strings.ToLower(binanceMarket)))
	// }
	for binanceMarket := range b.markets {
		reqParams = append(reqParams, fmt.Sprintf("%s@bookTicker", strings.ToLower(binanceMarket)))
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

	// res := struct {
	// 	Symbol   string          `json:"s"`
	// 	E        string          `json:"e"`
	// 	AskPrice decimal.Decimal `json:"a"`
	// 	BidPrice decimal.Decimal `json:"b"`
	// 	// Unused, present for correct unmarshalling
	// 	NotE uint64 `json:"E"`
	// 	NotA string `json:"A"`
	// 	NotB string `json:"B"`
	// }{}
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

			b.agent.strategies[b.markets[res.Symbol]].binanceStore.SetBestBidAndAsk(res.BidPrice, res.AskPrice)
		}
	}()

}

func (vegaClient *VegaDataClient) testGrpcAddresses() (ok bool) {

	// We need to re-write this to handle the case where all datanodes are down and unreachable.
	// Alternatively, standardize the clients and reconnect handlers with a new API client implementation.

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
			wg := &sync.WaitGroup{}
			wg.Add(1)
			go vegaClient.StreamVegaData(wg)
			log.Println("Waiting for new vega data streams")
			wg.Wait()
			log.Println("Finished waiting for new vega data streams")
			vegaClient.reconnecting = false
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

	// marketIds := reflect.ValueOf(agent.strategies).MapKeys()

	// Load initial data
	vegaClient.loadMarkets()
	vegaClient.loadMarketData()
	vegaClient.loadAccounts()
	// vegaClient.loadOrders()
	vegaClient.loadPositions()
	vegaClient.loadAssets()
	vegaClient.loadLiquidityProvisions()
	vegaClient.loadStakeToCcyVolume()

	// spew.Dump(d.s.v)

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

		v.agent.strategies[marketId].vegaStore.SetMarket(res.Market)
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

		v.agent.strategies[marketId].vegaStore.SetMarketData(res.MarketData)
	}
}

func (v *VegaDataClient) loadAccounts() {

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

func (v *VegaDataClient) loadOrders() {

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

func (v *VegaDataClient) loadPositions() {

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

func (v *VegaDataClient) loadAssets() {

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

func (v *VegaDataClient) loadLiquidityProvisions() {

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

func (v *VegaDataClient) loadStakeToCcyVolume() {

	res, err := v.svc.GetNetworkParameter(context.Background(), &apipb.GetNetworkParameterRequest{Key: "market.liquidity.stakeToCcyVolume"})
	if err != nil {
		log.Printf("Could not get stakeToCcyVolume net param: %v\n", err)
		v.handleGrpcReconnect()
		return
	}

	netParam := res.GetNetworkParameter()

	for _, marketId := range v.vegaMarkets {
		v.agent.strategies[marketId].vegaStore.SetStakeToCcyVolume(netParam)
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

func (v *VegaDataClient) streamAccounts() {

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

func (v *VegaDataClient) streamOrders() {

	for _, marketId := range v.vegaMarkets {

		stream, err := v.svc.ObserveOrders(context.Background(), &apipb.ObserveOrdersRequest{MarketIds: []string{marketId}, PartyIds: []string{v.agent.pubkey}})
		if err != nil {
			log.Printf("Failed to start Orders stream: %v\n", err)
			v.handleGrpcReconnect()
			return
		}

		// Empty store before we start stream in case of reconnect with stale orders
		v.agent.strategies[marketId].vegaStore.ClearOrders()

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

func (v *VegaDataClient) streamPositions() {

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
