package pow

import (
	"fmt"
	"log"
	"math"
	"sort"
	"sync"
	"vega-mm/wallets"

	"code.vegaprotocol.io/vega/libs/crypto"
	"golang.org/x/exp/maps"

	"github.com/google/uuid"

	coreapipb "code.vegaprotocol.io/vega/protos/vega/api/v1"
)

type worker struct {
	mu sync.RWMutex

	inCh chan *coreapipb.PoWStatistic

	powStats          *coreapipb.PoWStatistic
	pubkeys           map[uint32]string
	blockKeepFraction float64

	stores  map[string]PowStore
	signers map[string]*wallets.VegaSigner
}

type ProofOfWork struct {
	BlockHash   string
	BlockHeight uint64
	Difficulty  uint
	Nonce       uint64
	TxId        string
	Used        bool
}

func newWorker() *worker {
	return &worker{
		mu:                sync.RWMutex{},
		blockKeepFraction: 0.8,
	}
}

func (w *worker) Init(inCh chan *coreapipb.PoWStatistic, pubkeys map[uint32]string) {

}

func (w *worker) Start() {

	// Get PoW Statistics
	go func() {
		for stat := range w.inCh {

		}
	}()

}

func (w *worker) UpdatePows(stats *coreapipb.PoWStatistic) {

	coreapipb.SpamStatistics
	coreapipb.PoWBlockState
	blockStates := stats.GetBlockStates()
	numPastBlocks := stats.GetNumberOfPastBlocks()

	// Sort blockStates
	sort.Slice(blockStates, func(i, j int) bool {
		return blockStates[i].BlockHeight < blockStates[i].BlockHeight
	})

	// Log to confirm correct height order
	heights := []uint64{}
	for i := 0; i < len(blockStates); i++ {
		heights = append(heights, blockStates[i].BlockHeight)
	}
	log.Printf("Heights before drop: %v", heights)

	// Drop blocks less than keepFraction * numPastBlocks
	numToDrop := uint64(float64(numPastBlocks) * w.blockKeepFraction)
	blockStates = blockStates[numToDrop:]

	// Log to confirm correct heights dropped
	heights = nil
	for i := 0; i < len(blockStates); i++ {
		heights = append(heights, blockStates[i].BlockHeight)
	}
	log.Printf("Heights after drop: %v", heights)

	for _, pubkey := range maps.Values(w.pubkeys) {
		// Check current PoWs for pubkey (No lock because it's a write once read many map)
		w.signers[pubkey]

	}

}

func (w *worker) GeneratePow() {

	// We're going to generate PoWs for blocks in the most recent spam statistics result

	// Get pow statsitics from store

	w.mu.Lock()
	if w.lastBlock == nil {
		return
	}
	lb := *w.lastBlock
	w.lastBlock.NumGeneratePowCalls++
	if _, ok := w.powMap[lb.Height]; !ok {
		w.powMap[lb.Height] = []*ProofOfWork{}
	}

	// numPow := len(w.powMap[lb.Height])
	// numPow := 0
	// for _, pow := range w.powMap[lb.Height] {
	// 	if !pow.used {
	// 		numPow++
	// 	}
	// }
	// fmt.Printf("NumPow: %v\n", numPow)
	// if numPow == 0 {
	// }

	wg := sync.WaitGroup{}
	total := uint32(5)
	n := lb.NumGeneratePowCalls * total
	for i := n; i < n+total; i++ {
		// log.Printf("Generating proof at index %v at height %v\n", i, lb.Height)
		wg.Add(1)
		go func(i uint32) {
			// Get difficulty and adjust based on number of proofs for this block
			difficulty := uint(lb.SpamPowDifficulty + uint32(math.Floor(float64(i/lb.SpamPowNumTxPerBlock))))
			// log.Printf("Generating proof at index %v, with difficulty %v, at height %v\n", i, difficulty, lb.Height)
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
	w.mu.Unlock()
	wg.Wait()
	fmt.Printf("Calculated %v proofs of work for block with height %v and indexes %v - %v\n", total, lb.Height, n, n+total-1)

}

func (w *worker) RemoveOldPow() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.lastBlock == nil {
		return
	}

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
	// log.Printf("Using proof with txid %v height %v and difficulty %v\n", pow.txId, pow.blockHeight, pow.difficulty)
	return pow
}
