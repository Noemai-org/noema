package candidaterank

import (
	"errors"
	"math/big"
)

// ErrShare is returned by ShareBP for a share that is not in [0, 1]: a negative numerator, a denominator of 0 or less, or a numerator above the denominator.
var ErrShare = errors.New("candidaterank: not a share of one or less")

// WrittenBy is the Record identity that appends CANDIDATES_RESOLVED (A725: the ranking is Trust's, and writing it under another power would break the separation of the four powers). The
// call site must use it, and a test pins it to the Record's own set of emitters.
const WrittenBy = "core-trust"

// EventType is the Record event type of AuditPayload (PERD 32.2).
const EventType = "CANDIDATES_RESOLVED"

// ShareBP turns the exact share numerator/denominator of the candidates read (candidates-response.v2: recipient_match) into the recipient feature in basis points: floor division in integers,
// never rounded and never through a float, like every other feature (A725, A560). A share outside [0, 1] and a zero or negative denominator are errors, not clamped: a value that cannot be
// a share is a defect of the read, and a clamped one would be a score nobody computed. The product is widened, so the largest integers cannot overflow.
func ShareBP(numerator, denominator int64) (int64, error) {
	if numerator < 0 || denominator <= 0 || numerator > denominator {
		return 0, ErrShare
	}
	scaled := new(big.Int).Mul(big.NewInt(numerator), big.NewInt(UnitBP))
	return scaled.Quo(scaled, big.NewInt(denominator)).Int64(), nil
}
