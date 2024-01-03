package pow

import (
	"log"
	"math"
	// "sort"
	"sync"

	"code.vegaprotocol.io/vega/libs/crypto"
	"golang.org/x/exp/maps"

	"github.com/google/uuid"

	coreapipb "code.vegaprotocol.io/vega/protos/vega/api/v1"
)

type worker struct {
	mu sync.RWMutex

	inCh chan *RecentBlock

	powStats     map[string]*coreapipb.PoWStatistic
	recentBlocks map[uint64]*RecentBlock
	lastBlock    *RecentBlock

	agentPubkeys       []string
	numStratsPerPubkey map[string]int
	blockKeepFraction  float64

	stores map[string]*PowStore
}

type PowStatistic struct {
	PartyId string
	*coreapipb.PoWStatistic
}

type ProofOfWork struct {
	BlockHash   string
	BlockHeight uint64
	Difficulty  uint
	Nonce       uint64
	TxId        string
	ChainId     string
	Used        bool
}

type RecentBlock struct {
	Height               uint64
	Hash                 string
	SpamPowHashFunction  string
	SpamPowDifficulty    uint32
	SpamPowNumPastBlocks uint32
	SpamPowNumTxPerBlock uint32
	ChainId              string
	NumGeneratePowCalls  uint32
}

func NewWorker() *worker {
	return &worker{
		mu:                sync.RWMutex{},
		blockKeepFraction: 0.8,

		powStats: map[string]*coreapipb.PoWStatistic{},
	}
}

func (w *worker) Init(inCh chan *RecentBlock, numStratsPerPubkey map[string]int, powStores map[string]*PowStore) *worker {
	w.inCh = inCh
	w.numStratsPerPubkey = numStratsPerPubkey
	w.agentPubkeys = maps.Keys(numStratsPerPubkey)
	w.stores = powStores
	return w
}

func (w *worker) setPowStats(stats *PowStatistic) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.powStats[stats.PartyId] = stats.PoWStatistic
}

func (w *worker) SetLastBlock(block *RecentBlock) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastBlock = block
}

func (w *worker) Start() {
	// Receive PoW Statistics and generate proofs
	go func() {
		for recentBlock := range w.inCh {
			w.mu.RLock()
			if w.lastBlock == nil {
				w.lastBlock = recentBlock
				w.mu.RUnlock()
				continue
			}
			lb := *w.lastBlock
			w.mu.RUnlock()

			if recentBlock.Height <= lb.Height {
				continue
			}

			keepHeight := lb.Height - uint64(math.Floor((100 * w.blockKeepFraction)))

			w.SetLastBlock(recentBlock)
			w.GeneratePows(recentBlock)
			w.PrunePowStores(keepHeight)
		}
	}()
}

// func (w *worker) UpdatePows(stats *PowStatistic) {

// 	// If this is the first PoWStatistic we are receiving since startup we must
// 	// track the most recent height for which it is valid and truncate all
// 	// subsequent PoWStatistic responses such that their min height is greater
// 	// than the max height of the first response. This ensures we do not accidentally
// 	// breach the pow spam rules. This is necessary because we only query one set of
// 	// spam statistics and not one set for each pubkey.

// 	log.Printf("PowStats: %+v", stats)

// 	blockStates := stats.GetBlockStates()
// 	numPastBlocks := stats.GetNumberOfPastBlocks()

// 	// Sort blockStates
// 	sort.Slice(blockStates, func(i, j int) bool {
// 		return blockStates[i].BlockHeight < blockStates[i].BlockHeight
// 	})

// 	// Log to confirm correct height order
// 	heights := []uint64{}
// 	for i := 0; i < len(blockStates); i++ {
// 		heights = append(heights, blockStates[i].BlockHeight)
// 	}
// 	log.Printf("Heights before drop: %v", heights)

// 	// Drop blocks less than keepFraction * numPastBlocks
// 	numToDrop := uint64(float64(numPastBlocks) * w.blockKeepFraction)
// 	if len(blockStates) > int(numToDrop+1) {
// 		blockStates = blockStates[numToDrop:]
// 	}

// 	// Log to confirm correct heights dropped
// 	heights = nil
// 	for i := 0; i < len(blockStates); i++ {
// 		heights = append(heights, blockStates[i].BlockHeight)
// 	}
// 	log.Printf("Heights after keepBlockFraction drop: %v", heights)

// 	// Drop blocks with seenTransactions != 0
// 	// Note: This will not work, only works when we query one set of
// 	// 		 spam statistics for each public key. Because we are
// 	//		 sharing one spam statistics response between all keys
// 	//		 we cannot make use of the "TransactionsSeen" field.
// 	for i := len(blockStates) - 1; i >= 0; i-- {
// 		if blockStates[i].TransactionsSeen != 0 {
// 			copy(blockStates[i-1:], blockStates[i:])
// 			blockStates = blockStates[:len(blockStates)-1]
// 		}
// 	}
// 	log.Printf("Heights after seenTransactions drop: %v", heights)

// 	// Check max height for agent pow store
// 	var mostRecentStoreHeight uint64
// 	w.mu.RLock()
// 	pubkey := stats.PartyId
// 	maxHeight := w.stores[pubkey].GetMaxHeight()
// 	if mostRecentStoreHeight < maxHeight {
// 		mostRecentStoreHeight = maxHeight
// 	}
// 	w.mu.RUnlock()

// 	// w.GeneratePows(blockStates, pubkey, mostRecentStoreHeight)

// 	// w.PrunePowStores(blockStates[0].BlockHeight)

// }

// Generates two proofs of work for each pubkey at each height. If an agent will be submitting
// two or more transactions per block the this will need a rafactor to generate proofs
// of a higher difficulty.
func (w *worker) GeneratePows(block *RecentBlock) {

	type powBuffer struct {
		mu    *sync.RWMutex
		slice []*ProofOfWork
	}

	numProofsPerStrat := 2

	for _, pubkey := range w.agentPubkeys {
		wg := &sync.WaitGroup{}
		buf := &powBuffer{
			mu:    &sync.RWMutex{},
			slice: []*ProofOfWork{},
		}

		w.mu.RLock()
		numStrats := w.numStratsPerPubkey[pubkey]
		w.mu.RUnlock()

		for i := 0; i < numProofsPerStrat*numStrats; i++ {
			wg.Add(1)
			go func(i int, pubkey string, buf *powBuffer) {
				difficulty := uint(block.SpamPowDifficulty + uint32(math.Floor(float64(uint32(i)/block.SpamPowNumTxPerBlock))))
				txId, _ := uuid.NewRandom()
				nonce, _, _ := crypto.PoW(block.Hash, txId.String(), difficulty, block.SpamPowHashFunction)
				pow := &ProofOfWork{
					BlockHash:   block.Hash,
					BlockHeight: block.Height,
					Difficulty:  difficulty,
					Nonce:       nonce,
					TxId:        txId.String(),
					ChainId:     block.ChainId,
					Used:        false,
				}

				buf.mu.Lock()
				buf.slice = append(buf.slice, pow)
				buf.mu.Unlock()

				wg.Done()
			}(i, pubkey, buf)
		}

		wg.Wait()
		log.Printf("Generated %v pows for pubkey: %v", len(buf.slice), pubkey)
		// Flush pow buffer
		w.mu.Lock()
		w.stores[pubkey].SetPows(buf.slice)
		w.mu.Unlock()

	}
}

func (w *worker) PrunePowStores(keepHeight uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, store := range maps.Values(w.stores) {
		store.PrunePows(keepHeight)
	}
}
