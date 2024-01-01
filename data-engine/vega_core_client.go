package data

import (
	"context"
	"fmt"
	"log"
	"time"

	vegaApiPb "code.vegaprotocol.io/vega/protos/vega/api/v1"
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

	txInCh chan *commandspb.Transaction
}

func NewVegaCoreClient(coreAddrs []string) *VegaCoreClient {
	return &VegaCoreClient{
		grpcAddrs: coreAddrs,
	}
}

func (v *VegaCoreClient) TestVegaCoreAddrs() {

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

		grpcClient := vegaApiPb.NewCoreServiceClient(conn)

		_, err = grpcClient.LastBlockHeight(context.Background(), &vegaApiPb.LastBlockHeightRequest{})
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
		log.Printf("No successful connections to Vega Core APIs. Waiting 10s before retry\n")
		s.vegaCoreAddr = ""
		time.Sleep(time.Second * 10)
		return
	}

	fmt.Printf("Successes: %+v\n", successes)
	fmt.Printf("Failures: %v\n", failures)
	sort.Slice(successes, func(i, j int) bool {
		return successes[i].latencyMs < successes[j].latencyMs
	})
	fmt.Printf("Lowest latency core address was: %v with %vms latency\n", successes[0].addr, successes[0].latencyMs)
	fmt.Printf("Setting signer vegaCoreAddr to %v\n", successes[0])
	s.vegaCoreAddr = successes[0].addr

}

func (s *signer) SubmitTx(tx *commandspb.Transaction) *vegaApiPb.SubmitTransactionResponse {

	fmt.Printf("Submitting Tx...\n")
	req := &vegaApiPb.SubmitTransactionRequest{Tx: tx}
	conn, err := grpc.Dial(s.vegaCoreAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("Could not connect to Vega Core node: %v", err)
		s.HandleVegaCoreReconnect()
		return nil
	}
	defer conn.Close()
	coreSvc := vegaApiPb.NewCoreServiceClient(conn)
	res, err := coreSvc.SubmitTransaction(context.Background(), req)
	if err != nil {
		log.Printf("Could not submit transaction to Vega Core node: %v", err)
		s.HandleVegaCoreReconnect()
		return nil
	} else if !res.Success {
		log.Printf("Tx not successful: tx = %s; code = %d; data = %s\n", res.TxHash, res.Code, res.Data)
		// log.Printf("PoW for Tx: %+v\n", tx.Pow)
	} else {
		log.Printf("Tx successful: TxHash: %v\n", res.TxHash)
		// log.Printf("PoW for Tx: %+v\n", tx.Pow)
	}
	return res
}

func (s *signer) GetLastBlock() *recentBlock {

	lbhReq := &vegaApiPb.LastBlockHeightRequest{}
	conn, err := grpc.Dial(s.vegaCoreAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("Failed to connect to Vega Core node: %v\n", err)
		// We should switch to another vega node in this instance
		s.HandleVegaCoreReconnect()
		return nil
	}
	defer conn.Close()

	coreService := vegaApiPb.NewCoreServiceClient(conn)
	res, err := coreService.LastBlockHeight(context.Background(), lbhReq)
	if err != nil {
		log.Printf("Failed to get last block height from core node: %v\n", err)
		// Switch to another vega node addr
		s.HandleVegaCoreReconnect()
		return nil
	}

	return &recentBlock{
		Height:               res.GetHeight(),
		Hash:                 res.GetHash(),
		SpamPowHashFunction:  res.GetSpamPowHashFunction(),
		SpamPowDifficulty:    res.GetSpamPowDifficulty(),
		SpamPowNumPastBlocks: res.GetSpamPowNumberOfPastBlocks(),
		SpamPowNumTxPerBlock: res.GetSpamPowNumberOfTxPerBlock(),
		ChainId:              res.GetChainId(),
	}
}

func (s *signer) HandleVegaCoreReconnect() {

	log.Println("Attempting Vega Core reconnect.")

	if s.vegaCoreReconnecting {
		log.Println("Already reconnecting...")
		return // Return control to caller
	}
	s.vegaCoreReconnecting = true
	s.vegaCoreReconnChan <- struct{}{}

}

func (s *signer) RunVegaCoreReconnectHandler() {

	for {
		select {
		case <-s.vegaCoreReconnChan:
			log.Println("Recieved event on Vega Core reconn channel")

			s.vegaCoreAddr = ""
			for s.vegaCoreAddr == "" {
				s.TestVegaCoreAddrs()
			}

			log.Println("Found available Vega Core API. Finished reconnecting.")
			s.vegaCoreReconnecting = false
		}
	}

}
