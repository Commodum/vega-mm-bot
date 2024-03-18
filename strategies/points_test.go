package strats

import (
	"log"
	"testing"

	"github.com/shopspring/decimal"
)

func TestOrderSizing(t *testing.T) {

	targetVolume := decimal.NewFromInt(310)
	refPrice := decimal.NewFromInt(2)
	orderSizeBase := decimal.NewFromInt(2)
	numOrders := decimal.NewFromInt(5)

	unitlessOrderSizeSum := orderSizeBase.Pow(numOrders).Sub(decimal.NewFromInt(1))

	volumeFraction := targetVolume.Div(unitlessOrderSizeSum)

	// Function applies the exponential distribution to our orders
	// such that their sum is roughly equal to the targetVolume.
	sizeF := func(i int) decimal.Decimal {

		// size := targetVolume.Div(decimal.NewFromFloat(totalOrderSizeUnits).Mul(vegaRefPriceAdj)).Mul(strat.OrderSizeBase.Pow(decimal.NewFromInt(int64(i + 1))))

		unitlessOrderSize := orderSizeBase.Pow(decimal.NewFromInt(int64(i)))

		size := unitlessOrderSize.Mul(volumeFraction).Div(refPrice)

		return size
	}

	for i := 0; i < int(numOrders.BigInt().Int64()); i++ {
		log.Printf("Size [%d]: %d", i, sizeF(i).BigInt().Int64())
	}

}
