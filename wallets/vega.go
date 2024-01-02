package wallets

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync"
	"vega-mm/pow"

	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"
	"github.com/golang/protobuf/proto"
	"github.com/tyler-smith/go-bip39"
	slip10 "github.com/vegaprotocol/go-slip10"
	"golang.org/x/crypto/sha3"
)

type VegaKeyPair struct {
	pubKey  string
	privKey string
}

func (v *VegaKeyPair) PubKey() string {
	return v.pubKey
}

type EmbeddedVegaWallet struct {
	seed []byte
	keys map[uint64]*VegaKeyPair
}

func NewWallet(mnemonic string) *EmbeddedVegaWallet {
	if len(mnemonic) == 0 {
		log.Fatalf("Invalid mnemonic provided...")
	}

	sanitizedMnemonic := strings.Join(strings.Fields(mnemonic), " ")
	seed := bip39.NewSeed(sanitizedMnemonic, "")
	ew := &EmbeddedVegaWallet{
		seed: seed,
		keys: map[uint64]*VegaKeyPair{},
	}

	// Derive first key pair
	ew.GetKeyPair(1)

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

func (w *EmbeddedVegaWallet) GetKeyPair(index uint64) *VegaKeyPair {
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
	w.keys[index] = &VegaKeyPair{pubKey: hexPubKey, privKey: hexPrivKey}

	fmt.Printf("Derived key pair with pubkey value: %v\n", hexPubKey)

	// os.Exit(0)

	return w.keys[index]
}

func (w *EmbeddedVegaWallet) getKeyPairByPublicKey(pubKey string) (*VegaKeyPair, error) {
	pubKey = strings.ToLower(pubKey)
	for _, keyPair := range w.keys {
		if keyPair.pubKey == pubKey {
			return keyPair, nil
		}
	}
	return nil, errors.New(fmt.Sprintf("Could not find key pair for public key: %v", pubKey))
}

type Signer interface {
	BuildTx(string, *commandspb.InputData) *commandspb.Transaction
	SignInputData(string, []byte) string
}

type VegaSigner struct {
	mu sync.RWMutex

	txDataCh chan *commandspb.InputData
	txOutCh  chan *commandspb.Transaction
	keypair  *VegaKeyPair
	chainId  string
	powStore *pow.PowStore

	// pows    map[uint64][]*pow.ProofOfWork
}

func (s *VegaSigner) GetInChan() chan *commandspb.InputData {
	return s.txDataCh
}

func (s *VegaSigner) GetOutChan() chan *commandspb.Transaction {
	return s.txOutCh
}

func NewVegaSigner(keyPair *VegaKeyPair, txBroadcastCh chan *commandspb.Transaction) (signer *VegaSigner) {
	return &VegaSigner{
		mu: sync.RWMutex{},

		txDataCh: make(chan *commandspb.InputData, 5),
		txOutCh:  txBroadcastCh,

		keypair:  keyPair,
		powStore: pow.NewPowStore(keyPair.pubKey),
	}
}

func (s *VegaSigner) SignInputData(bundledInputData []byte) string {
	digest := sha3.Sum256(bundledInputData)
	privKey := s.keypair.privKey
	if len(privKey) > 64 {
		privKey = privKey[0:64]
	}
	key, _ := hex.DecodeString(privKey)
	sig := ed25519.Sign(ed25519.NewKeyFromSeed(key), digest[:])
	return hex.EncodeToString(sig)
}

func (s *VegaSigner) StartFetchLoop() {
	for inputData := range s.GetInChan() {
		s.BuildAndSendTx(inputData)
	}
}

func (s *VegaSigner) BuildAndSendTx(inputData *commandspb.InputData) {
	fmt.Printf("Building Tx...\n")
	keyPair := s.keypair
	chainId := s.getChainId()

	// We could do some retries here instead...
	pow, ok := s.powStore.GetPow()
	if !ok {
		log.Printf("Proof of work not available for pubkey: %v, dropping tx.", s.keypair.pubKey)
		return
	}

	inputData.BlockHeight = pow.BlockHeight
	inputData.Nonce = rand.Uint64()
	inputDataBytes, err := proto.Marshal(inputData)
	if err != nil {
		log.Fatalf("Failed to marshal inputData into bytes: %v", err)
	}
	bundledInputData := bytes.Join([][]byte{[]byte(chainId), inputDataBytes}, []byte{0x0})
	hexSig := s.SignInputData(bundledInputData)
	log.Printf("Signed input data with keypair with pubkey: %v", keyPair.pubKey)
	signature := &commandspb.Signature{
		Value:   hexSig,
		Algo:    "vega/ed25519",
		Version: 1,
	}
	proofOfWork := &commandspb.ProofOfWork{
		Tid: pow.TxId, Nonce: pow.Nonce,
	}
	s.txOutCh <- &commandspb.Transaction{
		Version:   commandspb.TxVersion_TX_VERSION_V3,
		Signature: signature,
		Pow:       proofOfWork,
		InputData: inputDataBytes,
		From:      &commandspb.Transaction_PubKey{PubKey: keyPair.pubKey},
	}
}

// func (s *VegaSigner) BuildTx(inputData *commandspb.InputData) *commandspb.Transaction {
// 	fmt.Printf("Building Tx...\n")
// 	keyPair := s.keypair
// 	chainId := s.getChainId()
// 	pow, ok := s.powStore.GetPow()
// 	if !ok {

// 	}

// 	inputData.BlockHeight = pow.BlockHeight
// 	inputData.Nonce = rand.Uint64()
// 	inputDataBytes, err := proto.Marshal(inputData)
// 	if err != nil {
// 		log.Fatalf("Failed to marshal inputData into bytes: %v", err)
// 	}
// 	bundledInputData := bytes.Join([][]byte{[]byte(chainId), inputDataBytes}, []byte{0x0})
// 	hexSig := s.SignInputData(bundledInputData)
// 	log.Printf("Signed input data with keypair with pubkey: %v", keyPair.pubKey)
// 	signature := &commandspb.Signature{
// 		Value:   hexSig,
// 		Algo:    "vega/ed25519",
// 		Version: 1,
// 	}
// 	proofOfWork := &commandspb.ProofOfWork{
// 		Tid: pow.TxId, Nonce: pow.Nonce,
// 	}
// 	return &commandspb.Transaction{
// 		Version:   commandspb.TxVersion_TX_VERSION_V3,
// 		Signature: signature,
// 		Pow:       proofOfWork,
// 		InputData: inputDataBytes,
// 		From:      &commandspb.Transaction_PubKey{PubKey: keyPair.pubKey},
// 	}
// }

func (s *VegaSigner) SubmitTx(tx *commandspb.Transaction) {
	s.txOutCh <- tx
}

func (s *VegaSigner) getChainId() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.chainId
}

func (s *VegaSigner) GetPowStore() *pow.PowStore {
	return s.powStore
}
