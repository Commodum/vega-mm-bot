package pow

import (
	"log"
	"math"
	"sort"
	"sync"

	"code.vegaprotocol.io/vega/libs/crypto"
	"golang.org/x/exp/maps"

	"github.com/google/uuid"

	coreapipb "code.vegaprotocol.io/vega/protos/vega/api/v1"
)

type worker struct {
	mu sync.RWMutex

	inCh chan *coreapipb.PoWStatistic

	powStats           *coreapipb.PoWStatistic
	pubkeys            map[uint32]string
	numStratsPerPubkey map[string]int
	blockKeepFraction  float64

	stores map[string]*PowStore
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

func (w *worker) Init(inCh chan *coreapipb.PoWStatistic, pubkeys map[uint32]string, numStratsPerPubkey map[string]uint32) {

	// Need to get powStores from the sigers that are assigned to each agent.

	// powStores := map[string]*PowStore{}
	// for _, key := range maps.Values(pubkeys) {
	// 	powStores[key] = &PowStore{
	// 		mu:     sync.RWMutex{},
	// 		pubKey: key,
	// 		pows:   map[uint64][]*ProofOfWork{},
	// 	}
	// }

}

func (w *worker) setPowStats(stats *coreapipb.PoWStatistic) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.powStats = stats
}

func (w *worker) Start() {

	// Receive PoW Statistics and generate proofs
	go func() {
		for stats := range w.inCh {
			w.UpdatePows(stats)
		}
	}()
}

func (w *worker) UpdatePows(stats *coreapipb.PoWStatistic) {

	// If this is the first PoWStatistic we are receiving since startup we must
	// track the most recent height for which it is valid and truncate all
	// subsequent PoWStatistic responses such that their min height is greater
	// than the max height of the first response. This ensures we do not accidentally
	// breach the pow spam rules. This is necessary because we only query one set of
	// spam statistics and not one set for each pubkey.

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
	log.Printf("Heights after keepBlockFraction drop: %v", heights)

	// Drop blocks with seenTransactions != 0
	// Note: This will not work, only works when we query one set of
	// 		 spam statistics for each public key. Because we are
	//		 sharing one spam statistics response between all keys
	//		 we cannot make use of the "TransactionsSeen" field.
	// for i := len(blockStates) - 1; i >= 0; i-- {
	// 	if blockStates[i].TransactionsSeen != 0 {
	// 		copy(blockStates[i-1:], blockStates[i:])
	// 		blockStates = blockStates[:len(blockStates)-1]
	// 	}
	// }
	// log.Printf("Heights after seenTransactions drop: %v", heights)

	// Check max height for current PoWs
	var mostRecentStoreHeight uint64
	w.mu.RLock()
	pubkeys := maps.Keys(w.stores)
	for _, pubkey := range pubkeys {
		maxHeight := w.stores[pubkey].GetMaxHeight()
		if mostRecentStoreHeight < maxHeight {
			mostRecentStoreHeight = maxHeight
		}
	}
	w.mu.RUnlock()

	w.GeneratePows(blockStates, pubkeys, mostRecentStoreHeight)

	w.PrunePowStores(blockStates[0].BlockHeight)

}

// Generates two proofs of work for each pubkey at each height. If an agent will be submitting
// two or more transactions per block the this will need a rafactor to generate proofs
// of a higher difficulty.
func (w *worker) GeneratePows(blockStates []*coreapipb.PoWBlockState, pubkeys []string, mostRecentStoreHeight uint64) {

	numProofsPerStrat := 2

	type powBuffer struct {
		mu    sync.RWMutex
		slice []*ProofOfWork
	}

	for _, pubkey := range pubkeys {
		numStrats := w.numStratsPerPubkey[pubkey]
		go func(pubkey string, numStrats int) {
			wg := sync.WaitGroup{}
			buf := &powBuffer{
				mu:    sync.RWMutex{},
				slice: []*ProofOfWork{},
			}

			for _, blockState := range blockStates {
				if blockState.BlockHeight <= mostRecentStoreHeight {
					continue
				}
				wg.Add(1)
				go func(blockState *coreapipb.PoWBlockState, buf *powBuffer) {
					for i := 0; i < numProofsPerStrat*int(numStrats); i++ {
						//difficulty := uint(blockState.Difficulty + uint64(math.Floor(float64(i/lb.SpamPowNumTxPerBlock))))
						difficulty := uint(blockState.Difficulty + uint64(math.Floor(float64(i/int(blockState.TxPerBlock)))))
						// difficulty := uint(blockState.Difficulty)
						// log.Printf("Generating proof at index %v, with difficulty %v, at height %v\n", i, difficulty, lb.Height)
						txId, _ := uuid.NewRandom()
						nonce, _, _ := crypto.PoW(blockState.BlockHash, txId.String(), difficulty, blockState.HashFunction)
						pow := &ProofOfWork{
							BlockHash:   blockState.BlockHash,
							BlockHeight: blockState.BlockHeight,
							Difficulty:  difficulty,
							Nonce:       nonce,
							TxId:        txId.String(),
							Used:        false,
						}

						buf.mu.Lock()
						buf.slice = append(buf.slice, pow)
						buf.mu.Unlock()
					}

					wg.Done()
				}(blockState, buf)
			}

			wg.Wait()

			// Flush powBuffer
			w.mu.Lock()
			w.stores[pubkey].SetPows(buf.slice)
			w.mu.Unlock()
		}(pubkey, numStrats)
	}

}

func (w *worker) PrunePowStores(keepHeight uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, store := range maps.Values(w.stores) {
		store.PrunePows(keepHeight)
	}
}
