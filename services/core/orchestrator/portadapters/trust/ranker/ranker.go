// Package ranker is the candidate ranking port of the Trust adapters (REQ-AMB-003, REQ-AMB-002; INV-01, INV-14, INV-21; AGENT-16): it scores candidates by the pack's weights and
// tolerances, in exact arithmetic, as the model service's reference does (understanding/resolution/ranking.py). It holds no weight, no tolerance and no floor: all come from the pack's
// `understanding.ranking` (exact decimal strings, never floats). It decides nothing: Trust sorts, floors and asks.
//
// Features, each an exact share in [0, 1]: amount falls in a straight line from 1 (exact) to 0 at the pack's tolerance ratio of the wanted amount; temporal is 1 inside the window and
// falls to 0 over the pack's tolerance in seconds; recipient is the match the candidates read computed in core (the request's surface against the name; no name is here); relationship is 1
// when the beneficiary source matched; type_direction is 1 when the direction is the expected one. A feature with nothing to compare scores 0, which can only lower a score.
//
// The score in basis points is the exact total times 10000, rounded DOWN, so rounding never lifts a candidate over the floor.
package ranker

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
)

// ErrConfig is returned when the pack's ranking data is absent or not exact: a weight that is not a decimal, weights that do not sum to one, a negative tolerance.
var ErrConfig = errors.New("ranker: the pack's ranking data is missing or not exact")

var features = [...]string{"amount", "recipient", "temporal", "relationship", "type_direction"}

// Ranker implements trustport.CandidateRanker.
type Ranker struct {
	weights   [len(features)]*big.Rat
	amountTol *big.Rat
	timeTol   *big.Rat
}

var _ trustport.CandidateRanker = (*Ranker)(nil)

// New reads `understanding.ranking` from the verified pack's bytes.
func New(pack []byte) (*Ranker, error) {
	var doc struct {
		Understanding *struct {
			Ranking struct {
				Weights              map[string]string `json:"weights"`
				AmountToleranceRatio string            `json:"amount_tolerance_ratio"`
				TimeToleranceSeconds int64             `json:"time_tolerance_seconds"`
			} `json:"ranking"`
		} `json:"understanding"`
	}
	if err := json.Unmarshal(pack, &doc); err != nil || doc.Understanding == nil {
		return nil, ErrConfig
	}
	rk := doc.Understanding.Ranking
	r := &Ranker{}
	total := new(big.Rat)
	for i, f := range features {
		w, ok := new(big.Rat).SetString(rk.Weights[f])
		if !ok || w.Sign() < 0 || len(rk.Weights) != len(features) {
			return nil, ErrConfig
		}
		r.weights[i] = w
		total.Add(total, w)
	}
	if total.Cmp(big.NewRat(1, 1)) != 0 {
		return nil, ErrConfig
	}
	var ok bool
	if r.amountTol, ok = new(big.Rat).SetString(rk.AmountToleranceRatio); !ok || r.amountTol.Sign() < 0 || rk.TimeToleranceSeconds < 0 {
		return nil, ErrConfig
	}
	r.timeTol = new(big.Rat).SetInt64(rk.TimeToleranceSeconds)
	return r, nil
}

// ramp is 1 at distance 0, falling in a straight line to 0 at the tolerance; 0 beyond it and when there is no tolerance.
func ramp(distance, tolerance *big.Rat) *big.Rat {
	if distance.Sign() == 0 {
		return big.NewRat(1, 1)
	}
	if tolerance.Sign() <= 0 || distance.Cmp(tolerance) >= 0 {
		return new(big.Rat)
	}
	return new(big.Rat).Sub(big.NewRat(1, 1), new(big.Rat).Quo(distance, tolerance))
}

func absDiff(a, b int64) *big.Rat {
	d := new(big.Rat).SetInt64(a)
	d.Sub(d, new(big.Rat).SetInt64(b))
	return d.Abs(d)
}

// Rank scores every candidate; the scores are in the order of the rows, one each.
func (r *Ranker) Rank(ctx context.Context, terms trustport.CandidateSearch, rows []contracts.CandidatesResponseV2Candidate) ([]trustport.CandidateScore, error) {
	if r == nil || r.timeTol == nil {
		return nil, ErrConfig
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]trustport.CandidateScore, 0, len(rows))
	for _, c := range rows {
		at, err := time.Parse(time.RFC3339, string(c.OccurredAt))
		if err != nil {
			return nil, err
		}
		f := [len(features)]*big.Rat{new(big.Rat), new(big.Rat), new(big.Rat), new(big.Rat), new(big.Rat)}
		if terms.HasAmount && terms.AmountMinor > 0 && string(c.Currency) == terms.Unit {
			wanted := new(big.Rat).SetInt64(terms.AmountMinor)
			f[0] = ramp(absDiff(int64(c.AmountMinor), terms.AmountMinor), new(big.Rat).Mul(r.amountTol, wanted))
		}
		if terms.CounterpartySurface != "" && c.RecipientMatch.Denominator > 0 {
			f[1] = big.NewRat(c.RecipientMatch.Numerator, c.RecipientMatch.Denominator)
		}
		if !terms.From.IsZero() && !terms.To.IsZero() {
			switch {
			case !at.Before(terms.From) && !at.After(terms.To):
				f[2] = big.NewRat(1, 1)
			default:
				gap := terms.From.Sub(at)
				if at.After(terms.To) {
					gap = at.Sub(terms.To)
				}
				f[2] = ramp(new(big.Rat).SetInt64(int64(gap/time.Second)), r.timeTol)
			}
		}
		if c.BeneficiaryAliasMatch != nil && *c.BeneficiaryAliasMatch {
			f[3] = big.NewRat(1, 1)
		}
		total := new(big.Rat)
		for i := range features {
			total.Add(total, new(big.Rat).Mul(r.weights[i], f[i]))
		}
		bp := new(big.Int).Quo(new(big.Int).Mul(total.Num(), big.NewInt(10_000)), total.Denom())
		out = append(out, trustport.CandidateScore{CandidateID: c.CandidateId, ScoreBP: bp.Int64()})
	}
	return out, nil
}
