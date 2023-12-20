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

type vegaKeyPair struct {
	pubKey  string
	privKey string
}

type embeddedWallet struct {
	seed []byte
	keys map[uint64]*vegaKeyPair
}

func newWallet(mnemonic string) *embeddedWallet {
	if len(mnemonic) == 0 {
		log.Fatalf("Invalid mnemonic provided...")
	}

	sanitizedMnemonic := strings.Join(strings.Fields(mnemonic), " ")
	seed := bip39.NewSeed(sanitizedMnemonic, "")
	ew := &embeddedWallet{
		seed: seed,
		keys: map[uint64]*vegaKeyPair{},
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

func (w *embeddedWallet) getKeyPair(index uint64) *vegaKeyPair {
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
	w.keys[index] = &vegaKeyPair{pubKey: hexPubKey, privKey: hexPrivKey}

	fmt.Printf("Derived key pair with pubkey value: %v\n", hexPubKey)

	// os.Exit(0)

	return w.keys[index]
}

func (w *embeddedWallet) getKeyPairByPublicKey(pubKey string) (*vegaKeyPair, error) {
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

	txOutCh  chan *commandspb.Transaction
	keypair  *vegaKeyPair
	chainId  string
	powStore *pow.PowStore

	// pows    map[uint64][]*pow.ProofOfWork
}

func NewVegaSigner(keyPair *vegaKeyPair) *VegaSigner {
	return &VegaSigner{
		keypair:  keyPair,
		powStore: pow.NewPowStore(keyPair.pubKey),
		// pows:    map[uint64][]*pow.ProofOfWork{},
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

func (s *VegaSigner) BuildTx(inputData *commandspb.InputData) *commandspb.Transaction {
	fmt.Printf("Building Tx...\n")
	keyPair := s.keypair
	chainId := s.getChainId()
	pow := s.powStore.GetPow()

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
	return &commandspb.Transaction{
		Version:   commandspb.TxVersion_TX_VERSION_V3,
		Signature: signature,
		Pow:       proofOfWork,
		InputData: inputDataBytes,
		From:      &commandspb.Transaction_PubKey{PubKey: keyPair.pubKey},
	}
}

func (s *VegaSigner) SubmitTx(tx *commandspb.Transaction) {
	s.txOutCh <- tx
}

func (s *VegaSigner) getChainId() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.chainId
}
