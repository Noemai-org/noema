package candidaterank

// REQ-AMB-003, INV-01, INV-21; ruling A555, A559, A560 (interim, relayed). The shared golden vectors (testdata/ranking_vectors.json) are loaded by this suite and by the Python
// reference suite (services/model/tests/test_ranking_vectors.py). All data is SYNTHETIC.

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

type vecFile struct {
	Label  string `json:"label"`
	Config struct {
		Weights struct {
			Amount        int64 `json:"amount"`
			Recipient     int64 `json:"recipient"`
			Temporal      int64 `json:"temporal"`
			Relationship  int64 `json:"relationship"`
			TypeDirection int64 `json:"type_direction"`
		} `json:"weights_bp"`
		FloorBP         int64 `json:"floor_bp"`
		MarginBP        int64 `json:"margin_bp"`
		MaxAboveFloor   int   `json:"max_above_floor"`
		AmountTolBP     int64 `json:"amount_tolerance_bp"`
		TimeToleranceSc int64 `json:"time_tolerance_seconds"`
	} `json:"config"`
	AmountRamps []struct {
		Name              string `json:"name"`
		Wanted            *int64 `json:"wanted_minor"`
		Candidate         int64  `json:"candidate_minor"`
		CandidateCurrency string `json:"candidate_currency"`
		Want              int64  `json:"want_bp"`
	} `json:"amount_ramps"`
	TemporalRamps []struct {
		Name       string `json:"name"`
		OccurredAt string `json:"occurred_at"`
		NoWindow   bool   `json:"no_window"`
		Want       int64  `json:"want_bp"`
	} `json:"temporal_ramps"`
	Scores []struct {
		Name     string `json:"name"`
		Features struct {
			Amount        int64 `json:"amount"`
			Recipient     int64 `json:"recipient"`
			Temporal      int64 `json:"temporal"`
			Relationship  int64 `json:"relationship"`
			TypeDirection int64 `json:"type_direction"`
		} `json:"features_bp"`
		Want int64 `json:"want_bp"`
	} `json:"scores"`
	Resolutions []vecResolution `json:"resolutions"`
}

type vecResolution struct {
	Name   string `json:"name"`
	Target struct {
		Amount    *int64   `json:"amount_minor"`
		Currency  string   `json:"currency"`
		Window    []string `json:"window"`
		Direction string   `json:"expected_direction"`
	} `json:"target"`
	Context struct {
		MaterialFlag bool `json:"material_flag"`
		LowASR       bool `json:"key_slot_low_asr"`
	} `json:"context"`
	Candidates []struct {
		ID          string `json:"id"`
		AmountMinor int64  `json:"amount_minor"`
		Currency    string `json:"currency"`
		OccurredAt  string `json:"occurred_at"`
		RecipientBP int64  `json:"recipient_bp"`
		AliasMatch  *bool  `json:"alias_match"`
		Direction   string `json:"direction"`
	} `json:"candidates"`
	Want struct {
		State  string `json:"state"`
		Ranked []struct {
			ID      string `json:"id"`
			ScoreBP int64  `json:"score_bp"`
		} `json:"ranked"`
		TopBP      int64    `json:"top_bp"`
		SecondBP   int64    `json:"second_bp"`
		MarginBP   int64    `json:"margin_bp"`
		AboveFloor int      `json:"above_floor"`
		WhyNot     []string `json:"why_not"`
		ResolvedID *string  `json:"resolved_id"`
	} `json:"want"`
}

func loadVectors(t *testing.T) vecFile {
	t.Helper()
	raw, err := os.ReadFile("testdata/ranking_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vecFile
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if v.Label != "SYNTHETIC" {
		t.Fatalf("the vectors are not labelled SYNTHETIC: %q", v.Label)
	}
	return v
}

func vecConfig(t *testing.T, v vecFile) Config {
	t.Helper()
	c := Config{
		Weights: Weights{
			AmountBP: v.Config.Weights.Amount, RecipientBP: v.Config.Weights.Recipient, TemporalBP: v.Config.Weights.Temporal,
			RelationshipBP: v.Config.Weights.Relationship, TypeDirectionBP: v.Config.Weights.TypeDirection,
		},
		FloorBP: v.Config.FloorBP, MarginBP: v.Config.MarginBP, MaxAboveFloor: v.Config.MaxAboveFloor,
		AmountToleranceBP: v.Config.AmountTolBP, TimeToleranceSeconds: v.Config.TimeToleranceSc, MaxRaw: 20,
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("the vectors' own configuration is refused: %v", err)
	}
	return c
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatal(err)
	}
	return at
}

const (
	vecWindowFrom = "2026-11-20T09:00:00Z"
	vecWindowTo   = "2026-11-20T10:00:00Z"
)

func TestTheAmountRampMatchesTheVectors(t *testing.T) {
	v := loadVectors(t)
	c := vecConfig(t, v)
	for _, tc := range v.AmountRamps {
		tgt := Target{HasAmount: tc.Wanted != nil, Currency: "NGN"}
		if tc.Wanted != nil {
			tgt.AmountMinor = *tc.Wanted
		}
		cand := Candidate{ID: "cand_x", AmountMinor: tc.Candidate, Currency: "NGN"}
		if tc.CandidateCurrency != "" {
			cand.Currency = tc.CandidateCurrency
		}
		if got := c.AmountFeatureBP(tgt, cand); got != tc.Want {
			t.Errorf("amount ramp %s: got %d bp, want %d", tc.Name, got, tc.Want)
		}
	}
}

func TestTheTemporalRampMatchesTheVectors(t *testing.T) {
	v := loadVectors(t)
	c := vecConfig(t, v)
	for _, tc := range v.TemporalRamps {
		tgt := Target{HasWindow: !tc.NoWindow, From: mustTime(t, vecWindowFrom), To: mustTime(t, vecWindowTo)}
		cand := Candidate{ID: "cand_x", OccurredAt: mustTime(t, tc.OccurredAt)}
		if got := c.TemporalFeatureBP(tgt, cand); got != tc.Want {
			t.Errorf("temporal ramp %s: got %d bp, want %d", tc.Name, got, tc.Want)
		}
	}
}

func TestTheScoreIsTheFlooredWeightedSumOfTheFeatures(t *testing.T) {
	v := loadVectors(t)
	c := vecConfig(t, v)
	for _, tc := range v.Scores {
		f := Features{AmountBP: tc.Features.Amount, RecipientBP: tc.Features.Recipient, TemporalBP: tc.Features.Temporal, RelationshipBP: tc.Features.Relationship, TypeDirectionBP: tc.Features.TypeDirection}
		if got := c.ScoreBP(f); got != tc.Want {
			t.Errorf("score %s: got %d bp, want %d", tc.Name, got, tc.Want)
		}
	}
}

func TestTheResolutionMatchesTheVectorsAtEveryThreshold(t *testing.T) {
	v := loadVectors(t)
	c := vecConfig(t, v)
	for _, tc := range v.Resolutions {
		tgt := Target{HasAmount: tc.Target.Amount != nil, Currency: tc.Target.Currency, ExpectedDirection: tc.Target.Direction}
		if tc.Target.Amount != nil {
			tgt.AmountMinor = *tc.Target.Amount
		}
		if len(tc.Target.Window) == 2 {
			tgt.HasWindow, tgt.From, tgt.To = true, mustTime(t, tc.Target.Window[0]), mustTime(t, tc.Target.Window[1])
		}
		var cands []Candidate
		for _, row := range tc.Candidates {
			cands = append(cands, Candidate{ID: row.ID, AmountMinor: row.AmountMinor, Currency: row.Currency, OccurredAt: mustTime(t, row.OccurredAt), RecipientBP: row.RecipientBP, AliasMatch: row.AliasMatch != nil && *row.AliasMatch, Direction: row.Direction})
		}
		got, err := c.Rank(cands, tgt, Context{MaterialFlag: tc.Context.MaterialFlag, KeySlotLowASR: tc.Context.LowASR})
		if err != nil {
			t.Errorf("%s: %v", tc.Name, err)
			continue
		}
		if string(got.State) != tc.Want.State {
			t.Errorf("%s: state %s, want %s", tc.Name, got.State, tc.Want.State)
		}
		if got.TopBP != tc.Want.TopBP || got.SecondBP != tc.Want.SecondBP || got.MarginBP != tc.Want.MarginBP || got.AboveFloor != tc.Want.AboveFloor {
			t.Errorf("%s: top %d second %d margin %d above %d, want %d %d %d %d", tc.Name, got.TopBP, got.SecondBP, got.MarginBP, got.AboveFloor, tc.Want.TopBP, tc.Want.SecondBP, tc.Want.MarginBP, tc.Want.AboveFloor)
		}
		if len(got.Ranked) != len(tc.Want.Ranked) {
			t.Errorf("%s: %d ranked, want %d", tc.Name, len(got.Ranked), len(tc.Want.Ranked))
		} else {
			for i, w := range tc.Want.Ranked {
				if got.Ranked[i].ID != w.ID || got.Ranked[i].ScoreBP != w.ScoreBP {
					t.Errorf("%s: rank %d is %s at %d, want %s at %d", tc.Name, i, got.Ranked[i].ID, got.Ranked[i].ScoreBP, w.ID, w.ScoreBP)
				}
			}
		}
		var why []string
		for _, w := range got.WhyNot {
			why = append(why, string(w))
		}
		if len(why) != len(tc.Want.WhyNot) {
			t.Errorf("%s: why %v, want %v", tc.Name, why, tc.Want.WhyNot)
		} else {
			for i := range why {
				if why[i] != tc.Want.WhyNot[i] {
					t.Errorf("%s: why %v, want %v", tc.Name, why, tc.Want.WhyNot)
				}
			}
		}
		wantResolved := ""
		if tc.Want.ResolvedID != nil {
			wantResolved = *tc.Want.ResolvedID
		}
		if got.ResolvedID != wantResolved {
			t.Errorf("%s: resolved %q, want %q", tc.Name, got.ResolvedID, wantResolved)
		}
	}
}

// The decision at each threshold is read off the vectors by name: one basis point below, at and above the margin and the floor, and the count of candidates above the floor at and
// past the most the pack allows. A vector file that lost one of these cases would let a changed boundary through.
func TestTheVectorsCoverBothThresholdsOnEachSide(t *testing.T) {
	v := loadVectors(t)
	have := map[string]string{}
	for _, tc := range v.Resolutions {
		have[tc.Name] = tc.Want.State
	}
	want := map[string]string{
		"margin_3499": "AMBIGUOUS", "margin_3500": "AUTO_RESOLVED", "margin_3501": "AUTO_RESOLVED",
		"floor_6999": "AMBIGUOUS", "floor_7000": "AUTO_RESOLVED", "floor_7001": "AUTO_RESOLVED",
		"four_above_floor": "AMBIGUOUS", "five_above_floor": "TOO_MANY_CANDIDATES",
	}
	for name, state := range want {
		if have[name] != state {
			t.Errorf("vector %s: state %q, want %q", name, have[name], state)
		}
	}
	if v.Config.FloorBP != 7000 || v.Config.MarginBP != 3500 {
		t.Errorf("the vectors' thresholds are %d and %d, want the pack's 7000 and 3500", v.Config.FloorBP, v.Config.MarginBP)
	}
}
