package main

import (
	// "fmt"
	// "log"
	"sync"

	// "github.com/gorilla/websocket"
	apipb "code.vegaprotocol.io/vega/protos/data-node/api/v2"
	vegapb "code.vegaprotocol.io/vega/protos/vega"
	"github.com/shopspring/decimal"
	"golang.org/x/exp/maps"
	// vegapb "vega-mm/protos/vega"
)

type VegaStore struct {
	mu sync.RWMutex

	marketId           string
	assets             map[string]*vegapb.Asset
	market             *vegapb.Market
	marketData         *vegapb.MarketData
	accounts           map[string]*apipb.AccountBalance
	orders             map[string]*vegapb.Order
	position           *vegapb.Position
	liquidityProvision *vegapb.LiquidityProvision

	marketDataUpdateCounter int64
}

type BinanceStore struct {
	mu sync.RWMutex

	market  string
	bestBid decimal.Decimal
	bestAsk decimal.Decimal
}

type DataStore struct {
	v map[string]*VegaStore
	b map[string]*BinanceStore
}

func newVegaStore(marketId string) *VegaStore {
	return &VegaStore{
		marketId:                marketId,
		assets:                  map[string]*vegapb.Asset{},
		accounts:                map[string]*apipb.AccountBalance{},
		orders:                  map[string]*vegapb.Order{},
		marketDataUpdateCounter: 0,
	}
}

func newBinanceStore(mkt string) *BinanceStore {
	return &BinanceStore{
		market: mkt,
	}
}

func newDataStore() *DataStore {
	return &DataStore{
		v: map[string]*VegaStore{},
		b: map[string]*BinanceStore{},
	}
}

func (v *VegaStore) SetMarketId(marketId string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.marketId = marketId
}

func (v *VegaStore) SetMarket(market *vegapb.Market) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.market = market
}

func (v *VegaStore) GetMarket() *vegapb.Market {

	// log.Printf("%v", v)

	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.market
}

func (v *VegaStore) SetAsset(asset *vegapb.Asset) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.assets[asset.Id] = asset
}

func (v *VegaStore) GetAsset(assetId string) *vegapb.Asset {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.assets[assetId]
}

func (v *VegaStore) SetMarketData(marketData *vegapb.MarketData) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.marketData = marketData
	v.marketDataUpdateCounter++
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
		v.accounts[acc.Type.String()+acc.Asset+acc.MarketId] = acc
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

func (v *VegaStore) SetLiquidityProvision(liquidityProvision *vegapb.LiquidityProvision) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.liquidityProvision = liquidityProvision
}

func (v *VegaStore) GetLiquidityProvision() *vegapb.LiquidityProvision {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.liquidityProvision
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
