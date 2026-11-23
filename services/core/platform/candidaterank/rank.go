package candidaterank

import (
	"errors"
	"math/big"
	"sort"
	"time"
)

// UnitBP is one whole in basis points: a feature of 10000 is a full match, a score of 10000 the best there is.
const UnitBP int64 = 10000

var (
	// ErrConfig is returned for a configuration that is not usable, and by a zero Config: nothing ranks without a validated one (AGENT-16).
	ErrConfig = errors.New("candidaterank: the configuration is not usable")
	// ErrCandidates is returned for a candidate set or a target that cannot be ranked: nothing is guessed (A332).
	ErrCandidates = errors.New("candidaterank: the candidates or the target are not usable")
	// ErrNumber is returned by ParseBP for text that is not an exact number of basis points.
	ErrNumber = errors.New("candidaterank: not an exact number of basis points")
)

// Weights are the pack's five weights in basis points. They total UnitBP exactly.
type Weights struct {
	AmountBP, RecipientBP, TemporalBP, RelationshipBP, TypeDirectionBP int64
}

// Config is everything the ranker needs, all of it pack data: there is no default for any part. FloorBP and MarginBP are the two thresholds of the auto-resolve rule.
type Config struct {
	Weights Weights
	// FloorBP is the absolute floor: the top score must reach it (at or above), and a candidate counts as above the floor at or above it.
	FloorBP int64
	// MarginBP is the least gap between the top score and the second (at or above) for the top to resolve on its own.
	MarginBP int64
	// MaxAboveFloor is the most candidates above the floor before the set is too many (more than this is TOO_MANY_CANDIDATES).
	MaxAboveFloor int
	// AmountToleranceBP is the tolerance of the amount feature as a share of the amount the customer said, in basis points.
	AmountToleranceBP int64
	// TimeToleranceSeconds is how far outside the customer's window a candidate may be and still score above 0, in seconds.
	TimeToleranceSeconds int64
	// MaxRaw is the pack's bound on candidates to rank.
	MaxRaw int
}

// Validate refuses a Config that is not usable. The zero Config is not usable.
func (c Config) Validate() error {
	w := c.Weights
	for _, v := range []int64{w.AmountBP, w.RecipientBP, w.TemporalBP, w.RelationshipBP, w.TypeDirectionBP} {
		if v < 0 {
			return ErrConfig
		}
	}
	if w.AmountBP+w.RecipientBP+w.TemporalBP+w.RelationshipBP+w.TypeDirectionBP != UnitBP {
		return ErrConfig
	}
	if c.FloorBP < 0 || c.FloorBP > UnitBP || c.MarginBP < 0 || c.MarginBP > UnitBP || c.AmountToleranceBP < 0 || c.AmountToleranceBP > UnitBP {
		return ErrConfig
	}
	if c.MaxAboveFloor < 1 || c.MaxRaw < 1 || c.TimeToleranceSeconds < 0 {
		return ErrConfig
	}
	return nil
}

// Candidate is one masked candidate with the recipient feature the caller's matcher gave it (0 to UnitBP). The matcher is not part of this package.
type Candidate struct {
	ID          string
	AmountMinor int64
	Currency    string
	OccurredAt  time.Time
	RecipientBP int64
	AliasMatch  bool
	Direction   string
}

// Target is what the customer described: the values the candidates are compared to. A part the customer did not say is absent, and its feature is 0.
type Target struct {
	HasAmount         bool
	AmountMinor       int64
	Currency          string
	HasWindow         bool
	From, To          time.Time
	ExpectedDirection string
}

// Context holds the two facts from outside the ranking that stop an auto-resolve.
type Context struct {
	MaterialFlag  bool
	KeySlotLowASR bool
}

// Features are the five feature scores of one candidate, in basis points.
type Features struct {
	AmountBP, RecipientBP, TemporalBP, RelationshipBP, TypeDirectionBP int64
}

// State is what the ranking found. It is a proposal: ranking never allows, clarifies or escalates.
type State string

const (
	NoCandidates      State = "NO_CANDIDATES"
	AutoResolved      State = "AUTO_RESOLVED"
	Ambiguous         State = "AMBIGUOUS"
	TooManyCandidates State = "TOO_MANY_CANDIDATES"
)

// Why is a rule that kept the top candidate from resolving on its own.
type Why string

const (
	TieAtTop          Why = "TIE_AT_TOP"
	MarginNotMet      Why = "MARGIN_NOT_MET"
	BelowFloor        Why = "BELOW_FLOOR"
	MaterialFlag      Why = "MATERIAL_FLAG"
	LowASRConfidence  Why = "LOW_ASR_CONFIDENCE"
	TooManyAboveFloor Why = "TOO_MANY_ABOVE_FLOOR"
)

// Scored is one candidate's score in basis points.
type Scored struct {
	ID      string
	ScoreBP int64
}

// Resolution is the result. Ranked is in presentation order: score, then the newer time, then the ID (REQ-AMB-004); an order never breaks a tie for resolving.
type Resolution struct {
	State      State
	Ranked     []Scored
	TopBP      int64
	SecondBP   int64
	MarginBP   int64
	AboveFloor int
	WhyNot     []Why
	ResolvedID string
}

// scaledScore is the exact weighted sum of the features, unrounded: the sum of weight x feature, each in basis points, so it is the score in basis points times UnitBP (an integer, at
// most UnitBP x UnitBP). The margin, the floor and the tie are decided on it and never on a floored score (A566).
func (c Config) scaledScore(f Features) int64 {
	w := c.Weights
	return w.AmountBP*f.AmountBP + w.RecipientBP*f.RecipientBP + w.TemporalBP*f.TemporalBP + w.RelationshipBP*f.RelationshipBP + w.TypeDirectionBP*f.TypeDirectionBP
}

// ScoreBP is the weighted sum of the features, floored once to a whole basis point: the score as it is reported. It is not what a threshold is compared with (A566).
func (c Config) ScoreBP(f Features) int64 {
	return c.scaledScore(f) / UnitBP
}

// ramp is the share of UnitBP left when `distance` of `tolerance` is used up: UnitBP at distance 0, falling in a straight line to 0 at the tolerance, floored; 0 beyond it and when
// there is no tolerance. Both arguments are non-negative and are widened, so that no product overflows.
func ramp(distance, tolerance *big.Int) int64 {
	if distance.Sign() == 0 {
		return UnitBP
	}
	if tolerance.Sign() <= 0 || distance.Cmp(tolerance) >= 0 {
		return 0
	}
	left := new(big.Int).Sub(tolerance, distance)
	left.Mul(left, big.NewInt(UnitBP))
	return left.Quo(left, tolerance).Int64()
}

// AmountFeatureBP is 0 when the customer named no amount or another currency; otherwise it ramps from UnitBP at the exact amount down to 0 at the tolerance.
func (c Config) AmountFeatureBP(t Target, cand Candidate) int64 {
	if !t.HasAmount || t.Currency == "" || t.Currency != cand.Currency {
		return 0
	}
	wanted := big.NewInt(t.AmountMinor)
	distance := new(big.Int).Sub(big.NewInt(cand.AmountMinor), wanted)
	distance.Abs(distance)
	// distance / (tol/UnitBP x wanted), scaled by UnitBP on both sides: no division is made before the last.
	scaledDistance := new(big.Int).Mul(distance, big.NewInt(UnitBP))
	scaledTolerance := new(big.Int).Mul(big.NewInt(c.AmountToleranceBP), wanted)
	return ramp(scaledDistance, scaledTolerance)
}

// TemporalFeatureBP is UnitBP inside the customer's window (both ends included), ramping to 0 at the tolerance outside it, in whole microseconds; 0 when the customer named no window.
func (c Config) TemporalFeatureBP(t Target, cand Candidate) int64 {
	if !t.HasWindow {
		return 0
	}
	if !cand.OccurredAt.Before(t.From) && !cand.OccurredAt.After(t.To) {
		return UnitBP
	}
	gap := t.From.Sub(cand.OccurredAt)
	if cand.OccurredAt.After(t.To) {
		gap = cand.OccurredAt.Sub(t.To)
	}
	micros := big.NewInt(int64(gap / time.Microsecond))
	tolerance := new(big.Int).Mul(big.NewInt(c.TimeToleranceSeconds), big.NewInt(int64(time.Second/time.Microsecond)))
	return ramp(micros, tolerance)
}

// RelationshipFeatureBP is UnitBP for a candidate that matches a saved beneficiary alias, and 0 for anything else (including an unknown).
func RelationshipFeatureBP(cand Candidate) int64 {
	if cand.AliasMatch {
		return UnitBP
	}
	return 0
}

// TypeDirectionFeatureBP is UnitBP when the customer said a direction and the candidate has it.
func TypeDirectionFeatureBP(t Target, cand Candidate) int64 {
	if t.ExpectedDirection != "" && t.ExpectedDirection == cand.Direction {
		return UnitBP
	}
	return 0
}

// Rank scores every candidate and applies the auto-resolve rule: exactly one candidate with the top score; top minus second at or above MarginBP (the second is 0 for a single
// candidate); top at or above FloorBP; no material flag and no low ASR confidence. More than MaxAboveFloor candidates at or above the floor is TOO_MANY_CANDIDATES, whatever else
// holds. A566: the tie, the margin and the floor are compared in exact scaled-integer arithmetic on the unrounded sums, never on floored scores (9000.00 against 5500.25 is a margin of
// 3499.75 and does not resolve, although the floored scores differ by exactly 3500); floor division is applied only to the scores that are reported, so the reported margin is the
// floor of the exact margin. The input is not changed and the result shares nothing with it. The order of the rows changes nothing.
func (c Config) Rank(cands []Candidate, t Target, ctx Context) (Resolution, error) {
	if err := c.Validate(); err != nil {
		return Resolution{}, err
	}
	if len(cands) > c.MaxRaw || (t.HasAmount && t.AmountMinor < 0) || (t.HasWindow && t.To.Before(t.From)) {
		return Resolution{}, ErrCandidates
	}
	seen := make(map[string]bool, len(cands))
	type row struct {
		id     string
		scaled int64
		at     time.Time
	}
	rows := make([]row, 0, len(cands))
	for _, cand := range cands {
		if cand.ID == "" || seen[cand.ID] || cand.AmountMinor < 0 || cand.RecipientBP < 0 || cand.RecipientBP > UnitBP || cand.OccurredAt.IsZero() {
			return Resolution{}, ErrCandidates
		}
		seen[cand.ID] = true
		f := Features{
			AmountBP: c.AmountFeatureBP(t, cand), RecipientBP: cand.RecipientBP, TemporalBP: c.TemporalFeatureBP(t, cand),
			RelationshipBP: RelationshipFeatureBP(cand), TypeDirectionBP: TypeDirectionFeatureBP(t, cand),
		}
		rows = append(rows, row{id: cand.ID, scaled: c.scaledScore(f), at: cand.OccurredAt})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].scaled != rows[j].scaled {
			return rows[i].scaled > rows[j].scaled
		}
		if !rows[i].at.Equal(rows[j].at) {
			return rows[i].at.After(rows[j].at)
		}
		return rows[i].id < rows[j].id
	})
	res := Resolution{Ranked: make([]Scored, 0, len(rows))}
	floorScaled, marginScaled := c.FloorBP*UnitBP, c.MarginBP*UnitBP
	for _, r := range rows {
		res.Ranked = append(res.Ranked, Scored{ID: r.id, ScoreBP: r.scaled / UnitBP})
		if r.scaled >= floorScaled {
			res.AboveFloor++
		}
	}
	if len(rows) == 0 {
		res.State = NoCandidates
		return res, nil
	}
	topScaled := rows[0].scaled
	var secondScaled int64
	if len(rows) > 1 {
		secondScaled = rows[1].scaled
	}
	res.TopBP, res.SecondBP, res.MarginBP = topScaled/UnitBP, secondScaled/UnitBP, (topScaled-secondScaled)/UnitBP
	if len(rows) > 1 && topScaled == secondScaled {
		res.WhyNot = append(res.WhyNot, TieAtTop)
	}
	if topScaled-secondScaled < marginScaled {
		res.WhyNot = append(res.WhyNot, MarginNotMet)
	}
	if topScaled < floorScaled {
		res.WhyNot = append(res.WhyNot, BelowFloor)
	}
	if ctx.MaterialFlag {
		res.WhyNot = append(res.WhyNot, MaterialFlag)
	}
	if ctx.KeySlotLowASR {
		res.WhyNot = append(res.WhyNot, LowASRConfidence)
	}
	if res.AboveFloor > c.MaxAboveFloor {
		res.WhyNot = append(res.WhyNot, TooManyAboveFloor)
	}
	switch {
	case res.AboveFloor > c.MaxAboveFloor:
		res.State = TooManyCandidates
	case len(res.WhyNot) > 0:
		res.State = Ambiguous
	default:
		res.State = AutoResolved
		res.ResolvedID = rows[0].id
	}
	return res, nil
}
