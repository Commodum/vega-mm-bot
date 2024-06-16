package strats

import (
	"fmt"
	"time"
	"vega-mm/metrics"

	vegapb "code.vegaprotocol.io/vega/protos/vega"
	"github.com/shopspring/decimal"
)

type RelativeReturnFarming *RelativeReturnFarmingOpts

type RelativeReturnFarmingOpts struct {
	Agent1 string
	Agent2 string

	PositionSize decimal.Decimal
}

type RelativeReturnFarmingStrategy struct {
	*BaseStrategy
	*RelativeReturnFarmingOpts
	*GeneralOpts

	Balance1 decimal.Decimal
	Balance2 decimal.Decimal

	Position1 vegapb.Position
	Position2 vegapb.Position
}

func NewRelativeReturnFarmingStrategy(opts *StrategyOpts[RelativeReturnFarming]) *RelativeReturnFarmingStrategy {
	return &RelativeReturnFarmingStrategy{
		BaseStrategy:              NewBaseStrategy(opts.General),
		GeneralOpts:               opts.General,
		RelativeReturnFarmingOpts: opts.Specific,
	}
}

func (strat *RelativeReturnFarmingStrategy) RunStrategy(metricsCh chan *metrics.MetricsEvent) {
	// This strategy is very simple. There are rewards being paid on the EGLP market to
	// traders with the best PnL relative to their position size. To farm there rewards
	// we will simply use two parties to open eaqual and opposite positions with
	// eachother. This way we are net neutral but if the market moves in one direction
	// then one of our accounts will earn rewards for having a positive PnL.
	//
	// Additionally, position size doesn't matter since the rewards are based on PnL
	// relative to the position size. Hence, we should use a very small position.
	//
	// We should also rebalance collateral between the two accounts. This might not
	// be necessary if we have very small positions with adequate margin, however, it
	// is good to have just in case.

	for range time.NewTicker(time.Second * 5).C {

		// If we have a position just report metrics and continue
		pos := strat.vegaStore.GetPosition()
		_ = pos

		// We need to decide on a price with which to open the position

		if strat.GetAgentPubKey() == strat.Agent1 {

			// If we have a position. Continue

		} else if strat.GetAgentPubKey() == strat.Agent2 {

		} else {
			panic(fmt.Sprintf("unknown pubkey (%s) running relative return farming strat", strat.GetAgentPubKey()))
		}

	}

}
