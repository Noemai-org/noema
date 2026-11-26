package candidaterank

// AMB-01: CANDIDATES_RESOLVED with scores. REQ-AMB-002 ("CANDIDATES_RESOLVED (IDs and scores, masked)"), REQ-AMB-003 ("Scores and feature values recorded"), PERD 32.1 (a payload holds
// integers only), INV-21, AGENT-13, A676 (no name anywhere). All data is SYNTHETIC.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

func auditSet() ([]Candidate, Target) {
	at := time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC)
	tgt := Target{HasAmount: true, AmountMinor: 100000, Currency: "NGN", HasWindow: true, From: at.Add(-30 * time.Minute), To: at.Add(30 * time.Minute), ExpectedDirection: "OUTGOING"}
	return []Candidate{
		{ID: "cand_low", AmountMinor: 100000, Currency: "NGN", OccurredAt: at, RecipientBP: 4000, Direction: "INCOMING"},
		{ID: "cand_top", AmountMinor: 100000, Currency: "NGN", OccurredAt: at, RecipientBP: 10000, AliasMatch: true, Direction: "OUTGOING"},
	}, tgt
}

func marshalAudit(t *testing.T, r Resolution) []byte {
	t.Helper()
	raw, err := json.Marshal(r.AuditPayload())
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// REQ-AMB-002, REQ-AMB-003: the payload carries every candidate's id and score in basis points, in the ranked order, with the five feature values, and the ranking's state, margin and counts.
func TestTheAuditPayloadCarriesIdsScoresAndFeaturesInBasisPoints(t *testing.T) {
	c := goodConfig()
	cands, tgt := auditSet()
	res, err := c.Rank(cands, tgt, Context{})
	if err != nil {
		t.Fatal(err)
	}
	p := res.AuditPayload()
	if p.State != string(res.State) || p.CandidateCount != 2 || p.AboveFloor != res.AboveFloor || p.TopBP != res.TopBP || p.SecondBP != res.SecondBP || p.MarginBP != res.MarginBP {
		t.Fatalf("the payload's summary %+v does not carry the resolution %+v", p, res)
	}
	if len(p.Candidates) != 2 || p.Candidates[0].CandidateID != "cand_top" || p.Candidates[1].CandidateID != "cand_low" {
		t.Fatalf("candidates = %+v, want cand_top then cand_low", p.Candidates)
	}
	top, low := p.Candidates[0], p.Candidates[1]
	if top.ScoreBP != 10000 || top.Features != (AuditFeatures{AmountBP: 10000, RecipientBP: 10000, TemporalBP: 10000, RelationshipBP: 10000, TypeDirectionBP: 10000}) {
		t.Errorf("top = %+v, want every feature and the score at 10000", top)
	}
	// 3500 (amount) + 2500 x 4000/10000 (recipient) + 2000 (time) = 6500; no alias and the wrong direction.
	if low.ScoreBP != 6500 || low.Features != (AuditFeatures{AmountBP: 10000, RecipientBP: 4000, TemporalBP: 10000}) {
		t.Errorf("low = %+v, want a score of 6500 from amount 10000, recipient 4000, time 10000", low)
	}
	if p.MarginBP != 3500 || p.State != string(AutoResolved) || p.ResolvedCandidateID != "cand_top" {
		t.Errorf("margin %d state %s resolved %q, want 3500, AUTO_RESOLVED and cand_top", p.MarginBP, p.State, p.ResolvedCandidateID)
	}
}

// REQ-AMB-003: the payload says why a set did not resolve, in the ranking's own order, and names a resolved candidate only when the ranking resolved on its own.
func TestTheAuditPayloadNamesAResolvedCandidateOnlyForAnAutoResolve(t *testing.T) {
	c := goodConfig()
	cands, tgt := auditSet()
	for _, tc := range []struct {
		name      string
		ctx       Context
		cands     []Candidate
		wantState State
		wantWhy   []string
	}{
		{"clean", Context{}, cands, AutoResolved, []string{}},
		{"material flag", Context{MaterialFlag: true}, cands, Ambiguous, []string{"MATERIAL_FLAG"}},
		{"low asr", Context{KeySlotLowASR: true}, cands, Ambiguous, []string{"LOW_ASR_CONFIDENCE"}},
		{"none", Context{}, nil, NoCandidates, []string{}},
	} {
		res, err := c.Rank(tc.cands, tgt, tc.ctx)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		p := res.AuditPayload()
		if p.State != string(tc.wantState) || fmt.Sprint(p.WhyNot) != fmt.Sprint(tc.wantWhy) {
			t.Errorf("%s: state %s why %v, want %s and %v", tc.name, p.State, p.WhyNot, tc.wantState, tc.wantWhy)
		}
		if (tc.wantState == AutoResolved) != (p.ResolvedCandidateID != "") {
			t.Errorf("%s: resolved id %q for state %s", tc.name, p.ResolvedCandidateID, p.State)
		}
	}
	// A tie at the top and a crowd: no resolved id either.
	at := time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC)
	tied := []Candidate{{ID: "cand_a", OccurredAt: at, RecipientBP: 9000}, {ID: "cand_b", OccurredAt: at, RecipientBP: 9000}}
	res, _ := c.Rank(tied, Target{}, Context{})
	if p := res.AuditPayload(); p.ResolvedCandidateID != "" || fmt.Sprint(p.WhyNot) != "[TIE_AT_TOP MARGIN_NOT_MET BELOW_FLOOR]" {
		t.Errorf("tie: resolved %q why %v", p.ResolvedCandidateID, p.WhyNot)
	}
}

// PERD 32.1: every number in a payload is an integer within 2^53-1 and no fraction or exponent is written. An empty set writes empty arrays, not null.
func TestTheAuditPayloadHoldsOnlyIntegersAndStrings(t *testing.T) {
	c := goodConfig()
	cands, tgt := auditSet()
	for name, in := range map[string][]Candidate{"two": cands, "none": nil} {
		res, err := c.Rank(in, tgt, Context{})
		if err != nil {
			t.Fatal(err)
		}
		raw := marshalAudit(t, res)
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			t.Fatal(err)
		}
		var walk func(path string, v any)
		walk = func(path string, v any) {
			switch x := v.(type) {
			case json.Number:
				n, err := x.Int64()
				if err != nil || strings.ContainsAny(string(x), ".eE") || n > 9007199254740991 || n < -9007199254740991 {
					t.Errorf("%s: %s: %s is not an integer within 2^53-1", name, path, x)
				}
			case map[string]any:
				for k, e := range x {
					walk(path+"."+k, e)
				}
			case []any:
				for i, e := range x {
					walk(fmt.Sprintf("%s[%d]", path, i), e)
				}
			case string, bool:
			case nil:
				t.Errorf("%s: %s is null: an empty list is written as [], an absent id is left out", name, path)
			default:
				t.Errorf("%s: %s has the unexpected type %T", name, path, v)
			}
		}
		walk("$", v)
	}
}

// AGENT-13, A676, REQ-AMB-002: the payload names exactly these keys: identifiers, scores and the ranking's own result. It has no name, amount, time, account digit or direction of a candidate.
func TestTheAuditPayloadHasNoKeyBeyondIdsAndScores(t *testing.T) {
	c := goodConfig()
	cands, tgt := auditSet()
	res, _ := c.Rank(cands, tgt, Context{})
	var v map[string]any
	if err := json.Unmarshal(marshalAudit(t, res), &v); err != nil {
		t.Fatal(err)
	}
	keys := func(m map[string]any) string {
		var ks []string
		for k := range m {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		return strings.Join(ks, ",")
	}
	if got, want := keys(v), "above_floor,candidate_count,candidates,margin_bp,resolved_candidate_id,second_bp,state,top_bp,why_not"; got != want {
		t.Errorf("payload keys = %s, want %s", got, want)
	}
	row := v["candidates"].([]any)[0].(map[string]any)
	if got, want := keys(row), "candidate_id,features,score_bp"; got != want {
		t.Errorf("candidate keys = %s, want %s", got, want)
	}
	if got, want := keys(row["features"].(map[string]any)), "amount_bp,recipient_bp,relationship_bp,temporal_bp,type_direction_bp"; got != want {
		t.Errorf("feature keys = %s, want %s", got, want)
	}
}

// INV-21: the payload is a copy. Changing it cannot change the resolution it came from, and two payloads of one resolution share nothing.
func TestTheAuditPayloadSharesNothingWithTheResolution(t *testing.T) {
	c := goodConfig()
	cands, tgt := auditSet()
	res, err := c.Rank(cands, tgt, Context{MaterialFlag: true})
	if err != nil {
		t.Fatal(err)
	}
	before := marshalAudit(t, res)
	p := res.AuditPayload()
	p.Candidates[0].CandidateID = "changed"
	p.WhyNot[0] = "changed"
	if res.Ranked[0].ID == "changed" || res.WhyNot[0] == "changed" {
		t.Fatal("the payload shares a slice with the resolution")
	}
	if again := marshalAudit(t, res); !bytes.Equal(before, again) {
		t.Fatalf("the payload changed after an edit of a copy:\n%s\n%s", before, again)
	}
}
