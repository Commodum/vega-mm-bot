package stores

import (
	"log"
	"sync"

	apipb "code.vegaprotocol.io/vega/protos/data-node/api/v2"
	vegapb "code.vegaprotocol.io/vega/protos/vega"
	// coreapipb "code.vegaprotocol.io/vega/protos/vega/api/v1"
	"github.com/shopspring/decimal"
	"golang.org/x/exp/maps"
)

type VegaStore struct {
	mu sync.RWMutex

	agentPubKey        string
	marketId           string
	assets             map[string]*vegapb.Asset
	market             *vegapb.Market
	marketData         *vegapb.MarketData
	accounts           map[string]*apipb.AccountBalance
	orders             map[string]*vegapb.Order
	externalOrders     map[string]*vegapb.Order
	position           *vegapb.Position
	liquidityProvision *vegapb.LiquidityProvision
	stakeToCcyVolume   string

	marketDataUpdateCounter int64
}

type BinanceStore struct {
	mu sync.RWMutex

	market  string
	bestBid decimal.Decimal
	bestAsk decimal.Decimal
	isStale bool
}

func NewVegaStore(marketId string) *VegaStore {
	return &VegaStore{
		mu: sync.RWMutex{},

		marketId:                marketId,
		assets:                  map[string]*vegapb.Asset{},
		accounts:                map[string]*apipb.AccountBalance{},
		orders:                  map[string]*vegapb.Order{},
		externalOrders:          map[string]*vegapb.Order{},
		marketDataUpdateCounter: 0,
	}
}

func NewBinanceStore(mkt string) *BinanceStore {
	return &BinanceStore{
		mu:     sync.RWMutex{},
		market: mkt,
	}
}

func (v *VegaStore) GetMarketDataUpdateCounter() int64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.marketDataUpdateCounter
}

func (v *VegaStore) SetAgentPubKey(pubkey string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.agentPubKey = pubkey
}

func (v *VegaStore) GetAgentPubKey() string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.agentPubKey
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

func (v *VegaStore) GetAllAssets() map[string]*vegapb.Asset {
	v.mu.Lock()
	defer v.mu.Unlock()

	assets := map[string]*vegapb.Asset{}
	for key, value := range v.assets {
		assets[key] = value
	}

	return assets
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

func (v *VegaStore) ClearOrders() {
	v.mu.Lock()
	defer v.mu.Unlock()

	for id := range v.orders {
		delete(v.orders, id)
	}

}

func (v *VegaStore) SetExternalOrders(orders []*vegapb.Order) {
	v.mu.Lock()
	defer v.mu.Unlock()

	var orderCount, deleteCount int
	for _, ord := range orders {
		if ord.Status != vegapb.Order_STATUS_ACTIVE {
			delete(v.externalOrders, ord.Id)
			deleteCount += 1
			continue
		}

		v.externalOrders[ord.Id] = ord

		orderCount += 1
	}
}

func (v *VegaStore) GetExternalOrders() []*vegapb.Order {
	v.mu.Lock()
	defer v.mu.Unlock()

	return maps.Values(v.externalOrders)
}

func (v *VegaStore) SetOrders(orders []*vegapb.Order) {
	v.mu.Lock()
	defer v.mu.Unlock()

	var orderCount, deleteCount int
	for _, ord := range orders {
		if ord.Status != vegapb.Order_STATUS_ACTIVE {
			delete(v.orders, ord.Id)
			deleteCount += 1
			continue
		}

		v.orders[ord.Id] = ord

		orderCount += 1
	}

	// log.Printf(`Finished setting orders. %v orders deleted\n`, deleteCount)
	// log.Printf(`Finished setting orders. %v orders recieved with "STATUS_ACTIVE"\n`, orderCount)
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

// func (v *VegaStore) SetLiquidityProvision(liquidityProvision *vegapb.LiquidityProvision) {
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

func (v *VegaStore) SetStakeToCcyVolume(netParam *vegapb.NetworkParameter) {
	if netParam.Key != "market.liquidity.stakeToCcyVolume" {
		log.Printf("Incorrect net param provided to SetStakeToCcyVolume func: %v\n", netParam)
		return
	}
	v.mu.Lock()
	v.stakeToCcyVolume = netParam.Value
	v.mu.Unlock()
}

func (v *VegaStore) GetStakeToCcyVolume() string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.stakeToCcyVolume
}

func (b *BinanceStore) GetMarketTicker() string {
	return b.market
}

func (b *BinanceStore) SetBestBidAndAsk(bid, ask decimal.Decimal) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.bestBid, b.bestAsk = bid, ask
}

func (b *BinanceStore) GetBestBid() (bid decimal.Decimal) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.bestBid.Copy()
}

func (b *BinanceStore) GetBestAsk() (ask decimal.Decimal) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.bestAsk.Copy()
}

func (b *BinanceStore) IsStale() bool {
	b.mu.RLock()
	b.mu.RUnlock()
	return b.isStale
}

func (b *BinanceStore) SetIsStale(isStale bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.isStale = isStale
}
