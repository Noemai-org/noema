package ranker_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/ranker"
)

// The weights and tolerances below are TEST values (SYNTHETIC), the adopted ones of PERD REQ-AMB-003; the ranker holds none of its own (INV-14, AGENT-16).
const packJSON = `{"understanding":{"ranking":{"weights":{"amount":"0.35","recipient":"0.25","temporal":"0.20","relationship":"0.10","type_direction":"0.10"},"amount_tolerance_ratio":"0.01","time_tolerance_seconds":7200}}}`

var t0 = time.Date(2026, 11, 18, 9, 30, 0, 0, time.UTC)

func row(id string, amount int64, at time.Time, num, den int64, alias bool) contracts.CandidatesResponseV2Candidate {
	a := alias
	return contracts.CandidatesResponseV2Candidate{CandidateId: id, AmountMinor: contracts.MoneyV1MinorAmount(amount), Currency: "NGN", OccurredAt: contracts.CommonV1Timestamp(at.Format(time.RFC3339)),
		CounterpartyRef: "cp_1", RecipientMatch: contracts.CandidatesResponseV2CandidateRecipientMatch{Numerator: num, Denominator: den}, BeneficiaryAliasMatch: &a}
}

func terms() trustport.CandidateSearch {
	return trustport.CandidateSearch{From: t0.Add(-time.Hour), To: t0.Add(time.Hour), HasAmount: true, AmountMinor: 100000, Unit: "NGN", CounterpartySurface: "ada"}
}

// REQ-AMB-003: an exact match on amount, recipient, window and relationship scores the sum of those weights; the expected direction is not given, so it scores 0 (a feature with nothing to
// compare can only lower a score).
func TestAnExactCandidateScoresTheSumOfItsFeaturesWeights(t *testing.T) {
	r, err := ranker.New([]byte(packJSON))
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Rank(context.Background(), terms(), []contracts.CandidatesResponseV2Candidate{row("cand_1", 100000, t0, 1, 1, true)})
	if err != nil || len(got) != 1 || got[0].ScoreBP != 9000 {
		t.Fatalf("scores %v, err %v; want 9000 bp (0.35+0.25+0.20+0.10)", got, err)
	}
}

// The amount feature falls in a straight line to 0 at the tolerance; the temporal one over the pack's seconds; both round the score DOWN.
func TestTheRampsAreExactAndRoundDown(t *testing.T) {
	r, _ := ranker.New([]byte(packJSON))
	half := row("cand_h", 100500, t0, 0, 1, false) // half the tolerance of 1%: amount 0.5
	late := row("cand_l", 100000, t0.Add(time.Hour+3600*time.Second), 0, 1, false)
	got, err := r.Rank(context.Background(), terms(), []contracts.CandidatesResponseV2Candidate{half, late})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].ScoreBP != 1750+2000 { // amount 0.35*0.5, temporal inside 0.20
		t.Errorf("half-tolerance score %d, want 3750", got[0].ScoreBP)
	}
	if got[1].ScoreBP != 3500+1000 { // amount 0.35, temporal 0.20 * (1 - 3600/7200)
		t.Errorf("late score %d, want 4500", got[1].ScoreBP)
	}
}

// A feature with nothing to compare scores 0: no amount, a currency that differs, no surface, no window.
func TestAFeatureWithNothingToCompareScoresZero(t *testing.T) {
	r, _ := ranker.New([]byte(packJSON))
	got, _ := r.Rank(context.Background(), trustport.CandidateSearch{}, []contracts.CandidatesResponseV2Candidate{row("cand_1", 100000, t0, 1, 1, false)})
	if got[0].ScoreBP != 0 {
		t.Errorf("score %d, want 0", got[0].ScoreBP)
	}
	other := terms()
	other.Unit = "USD"
	got, _ = r.Rank(context.Background(), other, []contracts.CandidatesResponseV2Candidate{row("cand_1", 100000, t0, 0, 1, false)})
	if got[0].ScoreBP != 2000 { // temporal only
		t.Errorf("currency mismatch score %d, want 2000", got[0].ScoreBP)
	}
}

// INV-21: the same input gives the same scores, in the order of the rows.
func TestTheSameInputGivesTheSameScores(t *testing.T) {
	r, _ := ranker.New([]byte(packJSON))
	rows := []contracts.CandidatesResponseV2Candidate{row("cand_1", 100500, t0, 1, 3, true), row("cand_2", 99000, t0.Add(-5*time.Hour), 2, 3, false)}
	first, _ := r.Rank(context.Background(), terms(), rows)
	for i := 0; i < 1000; i++ {
		again, _ := r.Rank(context.Background(), terms(), rows)
		if again[0] != first[0] || again[1] != first[1] {
			t.Fatalf("run %d differs: %v vs %v", i, again, first)
		}
	}
}

// AGENT-16: no default. Missing or inexact ranking data is refused.
func TestMissingOrInexactRankingDataIsRefused(t *testing.T) {
	for name, pack := range map[string]string{
		"no understanding":     `{}`,
		"weights not one":      strings.Replace(packJSON, `"0.35"`, `"0.36"`, 1),
		"a float weight":       strings.Replace(packJSON, `"0.35"`, `0.35`, 1),
		"a missing weight":     strings.Replace(packJSON, `"type_direction":"0.10"`, `"extra":"0.10"`, 1),
		"a negative tolerance": strings.Replace(packJSON, `"0.01"`, `"-0.01"`, 1),
	} {
		if _, err := ranker.New([]byte(pack)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
