package pow

import (
	"log"
	"sort"
	"sync"
	"time"

	"golang.org/x/exp/maps"
)

type PowStore struct {
	mu        sync.RWMutex
	pubKey    string
	pows      map[uint64][]*ProofOfWork
	minHeight uint64
	maxHeight uint64
}

func NewPowStore(pubKey string) *PowStore {
	return &PowStore{
		mu:     sync.RWMutex{},
		pubKey: pubKey,
		pows:   map[uint64][]*ProofOfWork{},
	}
}

func (p *PowStore) GetMaxHeight() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.maxHeight
}

func (p *PowStore) SetPows(proofs []*ProofOfWork) (n int64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, pow := range proofs {
		if _, ok := p.pows[pow.BlockHeight]; !ok {
			p.pows[pow.BlockHeight] = []*ProofOfWork{}
		}
		p.pows[pow.BlockHeight] = append(p.pows[pow.BlockHeight], pow)
		n += 1

		if pow.BlockHeight > p.maxHeight {
			p.maxHeight = pow.BlockHeight
		}
	}

	return
}

func (p *PowStore) GetPowWithRetry(retryInterval time.Duration, maxRetries int) (pow *ProofOfWork, ok bool) {
	retries := 0

	pow, ok = p.GetPow()

	for !ok {
		if retries >= maxRetries {
			return nil, false
		}
		log.Printf("No proofs of work available for pubkey: %v. Retrying...", p.pubKey)
		time.Sleep(retryInterval)
		pow, ok = p.GetPow()
		retries += 1
	}

	return pow, ok
}

func (p *PowStore) GetPow() (pow *ProofOfWork, ok bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	powSlice := []*ProofOfWork{}
	for _, pows := range p.pows {
		for _, pow := range pows {
			if !pow.Used {
				powSlice = append(powSlice, pow)
			}
		}
	}

	sort.Slice(powSlice, func(i, j int) bool {
		if powSlice[i].BlockHeight == powSlice[j].BlockHeight {
			return powSlice[i].Difficulty < powSlice[j].Difficulty
		}
		return powSlice[i].BlockHeight < powSlice[i].BlockHeight
	})

	availableProofs := len(powSlice)

	log.Printf("Total proofs remaining: %v\n", availableProofs)

	if availableProofs == 0 {
		return nil, false
	}

	proof := powSlice[0]
	p.taintPoW(proof.TxId, proof.BlockHeight)

	return proof, true

	// var minHeight, maxHeight uint64
	// minHeight, maxHeight = math.MaxUint64, 0

	// for _, height := range maps.Keys(p.pows) {
	// 	if height < minHeight {
	// 		minHeight = height
	// 	}
	// 	if height > maxHeight {
	// 		maxHeight = height
	// 	}
	// }

	// for i := p.minHeight; i <= p.maxHeight; i++ {
	// 	for _, pow := range p.pows[i] {
	// 		if !pow.Used {
	// 			p.taintPoW(pow.TxId, pow.BlockHeight)
	// 			return pow, true
	// 		}
	// 	}
	// }

}

func (p *PowStore) taintPoW(txId string, height uint64) {
	for _, pow := range p.pows[height] {
		if pow.TxId == txId {
			pow.Used = true
			return
		}
	}
}

func (p *PowStore) PrunePows(keepSinceHeight uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, height := range maps.Keys(p.pows) {
		if height < keepSinceHeight {
			delete(p.pows, height)
		}

		if p.minHeight < height {
			p.minHeight = height + 1
		}
	}
}
