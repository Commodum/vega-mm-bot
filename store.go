package main

import (
	// "fmt"
	"sync"

	// "github.com/gorilla/websocket"
	"github.com/shopspring/decimal"
	apipb "code.vegaprotocol.io/vega/protos/data-node/api/v2"
	vegapb "code.vegaprotocol.io/vega/protos/vega"
	"golang.org/x/exp/maps"
)

type VegaStore struct {
	mu sync.RWMutex

	marketId string
	market *vegapb.Market
	marketData *vegapb.MarketData
	accounts map[string]*apipb.AccountBalance
	orders map[string]*vegapb.Order
	position *vegapb.Position
}

type BinanceStore struct {
	mu sync.RWMutex

	market string
	bestBid decimal.Decimal
	bestAsk decimal.Decimal
}

type DataStore struct {
	v *VegaStore
	b *BinanceStore
}

func newVegaStore() *VegaStore {
	return &VegaStore{
		accounts: map[string]*apipb.AccountBalance{},
		orders: map[string]*vegapb.Order{},
	}
}

func newBinanceStore(mkt string) *BinanceStore {
	return &BinanceStore{
		market: mkt,
	}
}

func newDataStore(binanceMkt string) *DataStore {
	return &DataStore{
		v: newVegaStore(),
		b: newBinanceStore(binanceMkt),
	}
}

func (v *VegaStore) SetMarketId(marketId string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.marketId = marketId
}

func (v *VegaStore) GetMarketId() string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.marketId
}

func (v *VegaStore) SetMarket(market *vegapb.Market) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.market = market
}

func (v *VegaStore) GetMarket() *vegapb.Market {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.market
}

func (v *VegaStore) SetMarketData(marketData *vegapb.MarketData) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.marketData = marketData
}

func (v *VegaStore) GetMarketData() *vegapb.MarketData {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.marketData
}

func (v *VegaStore) SetAccounts(accounts []*apipb.AccountBalance) {
	v.mu.Lock()
	defer v.mu.Unlock()

	for _, acc := range accounts {
		v.accounts[acc.Type.String() + acc.Asset + acc.MarketId] = acc
	}
}

func (v *VegaStore) GetAccounts() []*apipb.AccountBalance {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return maps.Values(v.accounts)
}

func (v *VegaStore) SetOrders(orders []*vegapb.Order) {
	v.mu.Lock()
	defer v.mu.Unlock()

	for _, ord := range orders {
		if ord.Status != vegapb.Order_STATUS_ACTIVE {
			delete(v.orders, ord.Id)
			continue
		}
		
		v.orders[ord.Id] = ord
	}
}

func (v *VegaStore) GetOrders() []*vegapb.Order {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return maps.Values(v.orders)
}

func (v *VegaStore) SetPosition(position *vegapb.Position) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.position = position
}

func (v *VegaStore) GetPosition() *vegapb.Position {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.position
}

func (b *BinanceStore) Set(bid, ask decimal.Decimal) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.bestBid, b.bestAsk = bid, ask
}


func (b *BinanceStore) Get() (bid, ask decimal.Decimal) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.bestBid.Copy(), b.bestAsk.Copy()
}