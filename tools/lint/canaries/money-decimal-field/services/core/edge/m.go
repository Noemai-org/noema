package edge

import "math/big"

type Decimal struct{ digits string }

type Line struct {
	AmountMinor *big.Rat
	FeeMinor    Decimal
}
