package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand"

	// "os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"code.vegaprotocol.io/vega/libs/crypto"
	// "code.vegaprotocol.io/vega/libs/proto"
	apipb "code.vegaprotocol.io/vega/protos/data-node/api/v2"
	vegaApiPb "code.vegaprotocol.io/vega/protos/vega/api/v1"
	// vegapb "code.vegaprotocol.io/vega/protos/vega"
	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"

	// vegaWallet "code.vegaprotocol.io/vega/wallet/wallet"
	"github.com/golang/protobuf/proto"
	"github.com/google/uuid"
	"github.com/tyler-smith/go-bip39"
	slip10 "github.com/vegaprotocol/go-slip10"
	"golang.org/x/crypto/sha3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Let's define our workflow for creating, signing, and sending a transaction:
//	- Create/load wallet
//	- Continuously calculate PoW
//		- Get recent block
//		- Compute n proofs with increasing difficulty every "txPerBlock"
//		- Remove old proofs
//	- Build input data for command
//	- Build tx
//		- Sign input data
//		- Get PoW
//		- Assemble tx
//	- Submit tx to core node

type keyPair struct {
	pubKey  string
	privKey string
}

type embeddedWallet struct {
	seed []byte
	keys map[uint64]*keyPair
}

func newWallet(mnemonic string) *embeddedWallet {
	if len(mnemonic) == 0 {
		log.Fatalf("Invalid mnemonic provided...")
	}

	sanitizedMnemonic := strings.Join(strings.Fields(mnemonic), " ")
	seed := bip39.NewSeed(sanitizedMnemonic, "")
	ew := &embeddedWallet{
		seed: seed,
		keys: map[uint64]*keyPair{},
	}

	// Derive first key pair
	ew.getKeyPair(1)

	return ew

	// w, err := vegaWallet.ImportHDWallet("vega-mm-fairground", sanitizedMnemonic, vegaWallet.Version2)
	// if err != nil {
	// 	log.Printf("Failed to import HDWallet: %v", err)
	// }

	// firstKey, err := w.GenerateKeyPair(nil)
	// if err != nil {
	// 	log.Fatalf("Could not generate first key: %v", err)
	// }
}

func (w *embeddedWallet) getKeyPair(index uint64) *keyPair {
	if kp, ok := w.keys[index]; ok {
		return kp
	}

	// Derive key
	derivationPath := fmt.Sprintf("m/1789'/0'/%d'", index)
	node, err := slip10.DeriveForPath(derivationPath, w.seed)
	if err != nil {
		log.Fatalf("Could not derive key for path %v: %v", derivationPath, err)
	}

	pubKey, privKey := node.Keypair()
	hexPubKey, hexPrivKey := fmt.Sprintf("%x", pubKey), fmt.Sprintf("%x", privKey)
	w.keys[index] = &keyPair{pubKey: hexPubKey, privKey: hexPrivKey}

	fmt.Printf("Derived key pair with pubkey value: %v\n", hexPubKey)

	// os.Exit(0)

	return w.keys[index]
}

func (w *embeddedWallet) getKeyPairByPublicKey(pubKey string) (*keyPair, error) {
	pubKey = strings.ToLower(pubKey)
	for _, keyPair := range w.keys {
		if keyPair.pubKey == pubKey {
			return keyPair, nil
		}
	}
	return nil, errors.New(fmt.Sprintf("Could not find key pair for public key: %v", pubKey))
}

type proofOfWork struct {
	blockHash   string
	blockHeight uint64
	difficulty  uint
	nonce       uint64
	txId        string
	used        bool
}

type recentBlock struct {
	Height               uint64
	Hash                 string
	SpamPowHashFunction  string
	SpamPowDifficulty    uint32
	SpamPowNumPastBlocks uint32
	SpamPowNumTxPerBlock uint32
	ChainId              string
}

type Worker interface {
	GeneratePow()
	RemoveOldPow()
	GetPow() *proofOfWork
}

type worker struct {
	mu                sync.RWMutex
	lastBlock         *recentBlock
	numTxPerBlock     int
	numPastBlocks     int
	blockKeepFraction float64
	powMap            map[uint64][]*proofOfWork
}

func newWorker() Worker {
	return &worker{
		mu:                sync.RWMutex{},
		powMap:            map[uint64][]*proofOfWork{},
		blockKeepFraction: 0.8,
	}
}

func (w *worker) GeneratePow() {
	if w.lastBlock == nil {
		return
	}
	w.mu.Lock()
	lb := *w.lastBlock
	if _, ok := w.powMap[lb.Height]; !ok {
		w.powMap[lb.Height] = []*proofOfWork{}
	}
	numPow := len(w.powMap[lb.Height])
	w.mu.Unlock()

	fmt.Printf("NumPow: %v\n", numPow)

	if numPow == 0 {
		wg := sync.WaitGroup{}
		total := 6
		for i := 0; i < total; i++ {
			wg.Add(1)
			go func(i int) {
				// Get difficulty and adjust based on number of proofs for this block
				difficulty := uint(lb.SpamPowDifficulty + uint32(math.Floor(float64(i/int(lb.SpamPowNumTxPerBlock)))))
				txId, _ := uuid.NewRandom()
				nonce, _, _ := crypto.PoW(lb.Hash, txId.String(), difficulty, lb.SpamPowHashFunction)
				pow := &proofOfWork{
					blockHash:   lb.Hash,
					blockHeight: lb.Height,
					difficulty:  difficulty,
					nonce:       nonce,
					txId:        txId.String(),
					used:        false,
				}

				w.mu.Lock()
				w.powMap[lb.Height] = append(w.powMap[lb.Height], pow)
				w.mu.Unlock()
				wg.Done()
			}(i)
		}
		wg.Wait()
		fmt.Printf("Calculated %v proofs of work for block with height %v.\n", total, lb.Height)
	}

}

func (w *worker) RemoveOldPow() {
	if w.lastBlock == nil {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// log.Printf("numPastBlocks: %v\n", w.lastBlock.SpamPowNumPastBlocks)
	// log.Printf("numPastBlocks * keepFraction: %v\n", float64(w.lastBlock.SpamPowNumPastBlocks)*w.blockKeepFraction)
	// log.Printf("floor(numPastBlocks * keepFraction): %v\n", math.Floor(float64(w.lastBlock.SpamPowNumPastBlocks)*w.blockKeepFraction))

	minKeepHeight := w.lastBlock.Height - uint64(math.Floor(float64(w.lastBlock.SpamPowNumPastBlocks)*w.blockKeepFraction))
	log.Printf("MinKeepHeight: %v\n", minKeepHeight)
	for height := range w.powMap {
		if height < minKeepHeight {
			log.Printf("Deleting old proofs of work for height: %v\n", height)
			delete(w.powMap, height)
		}
	}
}

func (w *worker) TaintPow(txId string, height uint64) {
	// w.mu.RLock()
	// defer w.mu.RUnlock()

	for _, pow := range w.powMap[height] {
		if pow.txId == txId {
			pow.used = true
			return
		}
	}
}

func (w *worker) GetPow() *proofOfWork {

	// w.mu.RLock()
	powSlice := []*proofOfWork{}
	for _, pows := range w.powMap {
		for _, pow := range pows {
			if !pow.used {
				powSlice = append(powSlice, pow)
			}
		}
	}
	// w.mu.RUnlock()

	sort.Slice(powSlice, func(i, j int) bool {
		if powSlice[i].blockHeight == powSlice[j].blockHeight {
			return powSlice[i].difficulty < powSlice[j].difficulty
		}
		return powSlice[i].blockHeight < powSlice[i].blockHeight
	})

	availableProofs := len(powSlice)

	fmt.Printf("Total proofs remaining: %v\n", availableProofs)

	if availableProofs == 0 {
		// If this happens we could either wait for more or generate some more.
		log.Printf("No proofs of work available. Generating more...\n")
		// log.Printf("No proofs of work available... Waiting...\n")
		w.mu.Unlock()
		w.GeneratePow()
		// time.Sleep(time.Millisecond * 500)
		w.mu.Lock()
		return w.GetPow()
	}

	pow := powSlice[0]
	w.TaintPow(pow.txId, pow.blockHeight)
	return pow
}

func (s *signer) StartWorker() {

	// Fetch necessary net params (I think we can just use the values in the lastBlockRes instead)
	// s.SetWorkerNetParams()

	// Fetch last block
	go func() {
		for range time.NewTicker(time.Millisecond * 1000).C {
			if s.vegaCoreReconnecting {
				continue
			}
			lastBlock := s.GetLastBlock()
			s.worker.mu.Lock()
			s.worker.lastBlock = lastBlock
			s.worker.mu.Unlock()
		}
	}()

	// Generate PoW
	go func() {
		for range time.NewTicker(time.Millisecond * 1000).C {
			s.worker.GeneratePow()
		}
	}()

	// Prune old PoW
	go func() {
		for range time.NewTicker(time.Millisecond * 1000).C {
			s.worker.RemoveOldPow()
		}
	}()

}

type Signer interface {
	BuildTx(string, *commandspb.InputData) *commandspb.Transaction
	SignInputData(string, []byte) string
}

type signer struct {
	vegaCoreAddr         string
	vegaCoreAddrs        []string
	vegaCoreReconnecting bool
	vegaCoreReconnChan   chan struct{}
	wallet               *embeddedWallet
	worker               *worker
	agent                *agent
}

func newSigner(wallet *embeddedWallet, vegaCoreAddrs []string, agent *agent) Signer {
	signer := &signer{
		vegaCoreAddrs:        vegaCoreAddrs,
		vegaCoreReconnecting: false,
		vegaCoreReconnChan:   make(chan struct{}),
		wallet:               wallet,
		worker:               newWorker().(*worker),
		agent:                agent,
	}

	for signer.vegaCoreAddr == "" {
		signer.TestVegaCoreAddrs()
	}
	signer.StartWorker()

	return signer
}

func (s *signer) SetWorkerNetParams() {

	numPastBlocksRes, err := s.agent.vegaClient.svc.GetNetworkParameter(context.Background(), &apipb.GetNetworkParameterRequest{Key: "spam.pow.numberOfPastBlocks"})
	if err != nil {
		log.Fatalf("Failed to get numPastBlocks from data node: %v", err)
	}
	numTxPerBlockRes, err := s.agent.vegaClient.svc.GetNetworkParameter(context.Background(), &apipb.GetNetworkParameterRequest{Key: "spam.pow.numberOfTxPerBlock"})
	if err != nil {
		log.Fatalf("Failed to get numTxPerBlock from data node: %v", err)
	}

	s.worker.mu.Lock()
	defer s.worker.mu.Unlock()

	s.worker.numPastBlocks, err = strconv.Atoi(numPastBlocksRes.GetNetworkParameter().GetValue())
	if err != nil {
		log.Fatalf("Failed to convert net param string value to int: %v", err)
	}
	s.worker.numTxPerBlock, err = strconv.Atoi(numTxPerBlockRes.GetNetworkParameter().GetValue())
	if err != nil {
		log.Fatalf("Failed to convert net param string value to int: %v", err)
	}
}

func (s *signer) SignInputData(privKey string, bundledInputData []byte) string {
	digest := sha3.Sum256(bundledInputData)
	if len(privKey) > 64 {
		privKey = privKey[0:64]
	}
	key, _ := hex.DecodeString(privKey)
	sig := ed25519.Sign(ed25519.NewKeyFromSeed(key), digest[:])
	return hex.EncodeToString(sig)
}

func (s *signer) BuildTx(pubKey string, inputData *commandspb.InputData) *commandspb.Transaction {
	fmt.Printf("Building Tx...\n")
	s.worker.mu.Lock()
	lb := *s.worker.lastBlock
	pow := s.worker.GetPow()
	s.worker.mu.Unlock()

	keyPair, err := s.wallet.getKeyPairByPublicKey(pubKey)
	if err != nil {
		log.Fatalf("Failed to get pub key: %v", err)
	}

	inputData.BlockHeight = pow.blockHeight
	inputData.Nonce = rand.Uint64()
	inputDataBytes, err := proto.Marshal(inputData)
	if err != nil {
		log.Fatalf("Failed to marshal inputData into bytes: %v", err)
	}
	bundledInputData := bytes.Join([][]byte{[]byte(lb.ChainId), inputDataBytes}, []byte{0x0})
	hexSig := s.SignInputData(keyPair.privKey, bundledInputData)
	log.Printf("Signed input data with keypair with pubkey: %v", keyPair.pubKey)
	signature := &commandspb.Signature{
		Value:   hexSig,
		Algo:    "vega/ed25519",
		Version: 1,
	}
	proofOfWork := &commandspb.ProofOfWork{
		Tid: pow.txId, Nonce: pow.nonce,
	}
	return &commandspb.Transaction{
		Version:   commandspb.TxVersion_TX_VERSION_V3,
		Signature: signature,
		Pow:       proofOfWork,
		InputData: inputDataBytes,
		From:      &commandspb.Transaction_PubKey{PubKey: keyPair.pubKey},
	}
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
	} else {
		log.Printf("Tx successful: TxHash: %v", res.TxHash)
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

func (s *signer) TestVegaCoreAddrs() {

	fmt.Printf("Testing Vega core gRPC API addresses...\n")

	type successfulTest struct {
		addr      string
		latencyMs int64
	}

	// successes := []string{}
	successes := []successfulTest{}
	failures := []string{}

	for _, addr := range s.vegaCoreAddrs {

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

	// After successfully connecting to a Core API we should generate a burst of proofs of work

}
