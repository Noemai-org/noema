package edge

import "math/big"

// Allowed: arithmetic that is not a literal power of ten on money, and powers of ten on things that are not money.
type Line struct {
	AmountMinor int64
	ErrorRate   *big.Rat
}

func Double(amountMinor int64) int64 { return amountMinor * 2 }

func Sum(amountMinor, feeMinor int64) int64 { return amountMinor + feeMinor }

func Millis(elapsed int64) int64 { return elapsed * 1000 }

func Percent(count int64) int64 { return count / 100 }

func Scale(amountMinor, exponentFactor int64) int64 { return amountMinor * exponentFactor }
