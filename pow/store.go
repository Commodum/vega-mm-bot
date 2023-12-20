package pow

import (
	"math"
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
		pubKey: pubKey,
		pows:   map[uint64][]*ProofOfWork{},
	}
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
	}
	return
}

func (p *PowStore) GetPow() *ProofOfWork {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var minHeight, maxHeight uint64
	minHeight, maxHeight = math.MaxUint64, 0

	for _, height := range maps.Keys(p.pows) {
		if height < minHeight {
			minHeight = height
		}
		if height > maxHeight {
			maxHeight = height
		}
	}

	for i := minHeight; i <= maxHeight; i++ {
		for _, pow := range p.pows[i] {
			if !pow.Used {
				p.taintPoW(pow.TxId, pow.BlockHeight)
				return pow
			}
		}
	}

	// If we hit here then there were no pows available...
	time.Sleep(time.Millisecond * 100)
	return p.GetPow()
}

func (p *PowStore) taintPoW(txId string, height uint64) {
	for _, pow := range p.pows[height] {
		if pow.TxId == txId {
			pow.Used = true
			return
		}
	}
}

func (p *PowStore) PrunePows(oldestHeight uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, key := range maps.Keys(p.pows) {
		if key > oldestHeight {
			delete(p.pows, key)
		}
	}
}
