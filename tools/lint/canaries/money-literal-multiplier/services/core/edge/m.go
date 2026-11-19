package edge

// A literal exponent on money (M1): the exponent comes from contracts/currencies.json, never from a literal.
func ToMajor(amountMinor int64) int64 { return amountMinor / 100 }

func ToMinor(amount int64) int64 { return 1000 * amount }
