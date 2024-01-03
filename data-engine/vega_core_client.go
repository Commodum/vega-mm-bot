package data

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"
	"vega-mm/pow"

	coreapipb "code.vegaprotocol.io/vega/protos/vega/api/v1"
	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// This client is responsible for:
//   - Testing Vega Core gRPC addrs
//   - Fetching pow spam stats
//   - Submitting signed txs
//   - Reconnects
type VegaCoreClient struct {
	grpcAddr  string
	grpcAddrs []string

	agentPubkeys     []string
	txInCh           chan *commandspb.Transaction
	recentBlockOutCh chan *pow.RecentBlock

	reconnecting bool
	reconnChan   chan struct{}
}

func NewVegaCoreClient(coreAddrs []string) *VegaCoreClient {
	return &VegaCoreClient{
		grpcAddrs: coreAddrs,

		reconnChan: make(chan struct{}),
	}
}

func (v *VegaCoreClient) Init(pubkeys []string, txInCh chan *commandspb.Transaction, recentBlockCh chan *pow.RecentBlock) *VegaCoreClient {

	v.agentPubkeys = pubkeys
	v.txInCh = txInCh
	v.recentBlockOutCh = recentBlockCh

	ok := v.TestVegaCoreAddrs()
	if !ok {
		log.Fatalf("Failed to connect to a Vega core node.")
	}

	return v
}

func (v *VegaCoreClient) Start() {
	v.StartGetLastBlockLoop()
	v.StartSubmitTxLoop()
}

func (v *VegaCoreClient) TestVegaCoreAddrs() (ok bool) {

	fmt.Printf("Testing Vega core gRPC API addresses...\n")

	type successfulTest struct {
		addr      string
		latencyMs int64
	}

	// successes := []string{}
	successes := []successfulTest{}
	failures := []string{}

	for _, addr := range v.grpcAddrs {

		startTimeMs := time.Now().UnixMilli()

		conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Printf("Could not open connection to core node (%v): %v", addr, err)
			failures = append(failures, addr)
			continue
		}

		grpcClient := coreapipb.NewCoreServiceClient(conn)

		_, err = grpcClient.LastBlockHeight(context.Background(), &coreapipb.LastBlockHeightRequest{})
		if err != nil {
			log.Printf("Could not get last block from url: %v. Error: %v", addr, err)
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

	if len(successes) == 0 {
		ok = false
		v.grpcAddr = ""
		// log.Printf("No successful connections to Vega Core APIs. Waiting 10s before retry\n")
		// time.Sleep(time.Second * 10)
		return
	} else {
		ok = true
	}

	fmt.Printf("Successes: %+v\n", successes)
	fmt.Printf("Failures: %v\n", failures)
	sort.Slice(successes, func(i, j int) bool {
		return successes[i].latencyMs < successes[j].latencyMs
	})
	fmt.Printf("Lowest latency core address was: %v with %vms latency\n", successes[0].addr, successes[0].latencyMs)
	fmt.Printf("Setting signer vegaCoreAddr to %v\n", successes[0])
	v.grpcAddr = successes[0].addr
	return
}

func (v *VegaCoreClient) StartSubmitTxLoop() {

	conn, err := grpc.Dial(v.grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("Could not connect to Vega Core node: %v", err)
		v.HandleVegaCoreReconnect()
		return
	}

	go func(conn *grpc.ClientConn) {
		defer conn.Close()
		coreSvc := coreapipb.NewCoreServiceClient(conn)
		for tx := range v.txInCh {
			req := &coreapipb.SubmitTransactionRequest{Tx: tx}
			res, err := coreSvc.SubmitTransaction(context.Background(), req)
			if err != nil {
				log.Printf("Could not submit transaction to Vega Core node: %v", err)
				v.HandleVegaCoreReconnect()
				return
			} else if !res.Success {
				log.Printf("Tx not successful: txHash = %s; code = %d; data = %s\n", res.TxHash, res.Code, res.Data)
				// log.Printf("PoW for Tx: %+v\n", tx.Pow)
			} else {
				log.Printf("Tx successful: TxHash: %v\n", res.TxHash)
				// log.Printf("PoW for Tx: %+v\n", tx.Pow)
			}
		}
	}(conn)

}

func (v *VegaCoreClient) StartGetLastBlockLoop() {

	conn, err := grpc.Dial(v.grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("Failed to connect to Vega Core node: %v\n", err)
		// We should switch to another vega node in this instance
		v.HandleVegaCoreReconnect()
		return
	}

	go func(conn *grpc.ClientConn) {
		defer conn.Close()
		coreService := coreapipb.NewCoreServiceClient(conn)
		lbhReq := &coreapipb.LastBlockHeightRequest{}
		for range time.NewTicker(time.Millisecond * 600).C {

			res, err := coreService.LastBlockHeight(context.Background(), lbhReq)
			if err != nil {
				log.Printf("Failed to get last block height from core node: %v\n", err)
				// Switch to another vega node addr
				v.HandleVegaCoreReconnect()
				return
			}

			v.recentBlockOutCh <- &pow.RecentBlock{
				Height:               res.GetHeight(),
				Hash:                 res.GetHash(),
				SpamPowHashFunction:  res.GetSpamPowHashFunction(),
				SpamPowDifficulty:    res.GetSpamPowDifficulty(),
				SpamPowNumPastBlocks: res.GetSpamPowNumberOfPastBlocks(),
				SpamPowNumTxPerBlock: res.GetSpamPowNumberOfTxPerBlock(),
				ChainId:              res.GetChainId(),
			}

		}
	}(conn)

}

func (v *VegaCoreClient) HandleVegaCoreReconnect() {

	log.Println("Attempting Vega Core reconnect.")

	if v.reconnecting {
		log.Println("Already reconnecting...")
		return // Return control to caller
	}
	v.reconnecting = true
	v.reconnChan <- struct{}{}

}

func (v *VegaCoreClient) RunVegaCoreReconnectHandler() {

	for {
		select {
		case <-v.reconnChan:
			log.Println("Recieved event on Vega Core reconn channel")

			v.grpcAddr = ""
			ok := false
			for !ok {
				ok = v.TestVegaCoreAddrs()
				if !ok {
					n := 30
					log.Printf("Failed to reconnect to a Vega core node. Waiting %v seconds before retry", n)
					time.Sleep(time.Second * 30)
				}
			}

			log.Println("Found available Vega Core API. Finished reconnecting.")
			v.reconnecting = false

			// Restart the submit tx loop after successful reconnect.
			v.StartSubmitTxLoop()
			v.StartGetLastBlockLoop()
		}
	}

}
