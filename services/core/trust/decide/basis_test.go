package decide_test

// TRU-01: the Decision carries the pack version, the policy version of each rule that decided, and the evidence it rested on
// (REQ-TRU-001, INV-09). An ALLOW or a CONFIRM without all of them is invalid (fail closed).

import (
	"errors"
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

func fullBasis() decide.Basis {
	return decide.Basis{
		PackVersion:        "fin.pack@1.0.0",
		PolicyRules:        []decide.PolicyRuleVersion{{RuleID: "fin.pol.default.permit_reads", PolicyID: "fin.pol", Version: "3.1"}},
		EvidenceSnapshotID: "snap_01",
		EvidenceFreshness:  []decide.Freshness{{EvidenceID: "ev_1", EffectiveAt: 1_700_000_000}},
		EvaluatedAt:        1_700_000_100,
	}
}

func minimalBasis() decide.Basis {
	return decide.Basis{PackVersion: "fin.pack@1.0.0", EvaluatedAt: 1_700_000_100}
}

func resolvedIntent(t *testing.T) decide.ResolvedIntent {
	t.Helper()
	r, ok := decide.Admit(decide.IntentView{Version: 3, RequiredSlots: map[string]decide.SlotStatus{"s": decide.SlotResolved}}).Resolved()
	if !ok {
		t.Fatal("not resolved")
	}
	return r
}

func TestWithBasisCarriesPackPolicyAndEvidence(t *testing.T) {
	d := must(resolvedIntent(t).Allow(allPass))
	if d.HasCompleteBasis() {
		t.Fatal("a decision built by a constructor has no basis yet")
	}
	got, err := d.WithBasis(fullBasis())
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasCompleteBasis() || !reflect.DeepEqual(got.Basis(), fullBasis()) {
		t.Fatalf("basis not carried: %+v", got.Basis())
	}
	b := got.Basis() // the accessor returns copies: changing them changes nothing
	b.PolicyRules[0].Version = "9.9"
	b.EvidenceFreshness[0].EvidenceID = "tampered"
	if !reflect.DeepEqual(got.Basis(), fullBasis()) {
		t.Fatal("the basis was changed through an accessor")
	}
	if got.Outcome() != decide.Allow || got.IntentVersion() != 3 {
		t.Fatalf("the decision itself changed: %s v%d", got.Outcome(), got.IntentVersion())
	}
}

func TestBasisRefusals(t *testing.T) {
	mutate := func(f func(*decide.Basis)) decide.Basis { b := fullBasis(); f(&b); return b }
	cases := map[string]decide.Basis{
		"no pack version":             mutate(func(b *decide.Basis) { b.PackVersion = "" }),
		"no evaluated_at":             mutate(func(b *decide.Basis) { b.EvaluatedAt = 0 }),
		"policy rule without id":      mutate(func(b *decide.Basis) { b.PolicyRules[0].RuleID = "" }),
		"policy rule without policy":  mutate(func(b *decide.Basis) { b.PolicyRules[0].PolicyID = "" }),
		"policy rule without version": mutate(func(b *decide.Basis) { b.PolicyRules[0].Version = "" }),
		"freshness without evidence":  mutate(func(b *decide.Basis) { b.EvidenceFreshness[0].EvidenceID = "" }),
		"freshness without time":      mutate(func(b *decide.Basis) { b.EvidenceFreshness[0].EffectiveAt = 0 }),
	}
	for name, b := range cases {
		if _, err := must(resolvedIntent(t).Reject(allPass, core(t, "NO_PERMITTING_RULE"))).WithBasis(b); !errors.Is(err, decide.ErrBasisInvalid) {
			t.Errorf("%s: want ErrBasisInvalid, got %v", name, err)
		}
	}
	if _, err := (decide.Decision{}).WithBasis(fullBasis()); !errors.Is(err, decide.ErrBasisInvalid) {
		t.Errorf("a zero decision: %v", err)
	}
	d := must(must(resolvedIntent(t).Allow(allPass)).WithBasis(fullBasis()))
	if _, err := d.WithBasis(fullBasis()); !errors.Is(err, decide.ErrBasisInvalid) {
		t.Errorf("a basis cannot be replaced: %v", err)
	}
}

// REQ-TRU-001 / DEC-2026-10-18: an ALLOW or a CONFIRM rests on policy and evidence, so it needs all of them.
func TestAllowAndConfirmNeedAFullBasis(t *testing.T) {
	r := resolvedIntent(t)
	builders := map[string]func() decide.Decision{
		"ALLOW":   func() decide.Decision { return must(r.Allow(allPass)) },
		"CONFIRM": func() decide.Decision { return must(r.Confirm(allPass)) },
	}
	partial := map[string]func(*decide.Basis){
		"no policy rule": func(b *decide.Basis) { b.PolicyRules = nil },
		"no snapshot":    func(b *decide.Basis) { b.EvidenceSnapshotID = "" },
		"no freshness":   func(b *decide.Basis) { b.EvidenceFreshness = nil },
	}
	for name, build := range builders {
		for what, f := range partial {
			b := fullBasis()
			f(&b)
			if _, err := build().WithBasis(b); !errors.Is(err, decide.ErrBasisIncomplete) {
				t.Errorf("%s with %s: want ErrBasisIncomplete, got %v", name, what, err)
			}
		}
		if got, err := build().WithBasis(fullBasis()); err != nil || !got.HasCompleteBasis() {
			t.Errorf("%s with a full basis: %v", name, err)
		}
	}
}

// A refusal before any evidence is read (T1 to T6) has no snapshot and no policy: the minimal basis is enough.
func TestOtherOutcomesNeedOnlyThePackAndTheTime(t *testing.T) {
	r := resolvedIntent(t)
	unresolved, _ := decide.Admit(decide.IntentView{Version: 3, MaterialFlags: []string{"AMBIGUOUS_COUNTERPARTY"}}).Unresolved()
	ds := map[string]decide.Decision{
		"REJECT":   must(r.Reject(allPass, core(t, "NO_PERMITTING_RULE"))),
		"ESCALATE": must(r.Escalate(allPass, core(t, "INSUFFICIENT_EVIDENCE"))),
		"UNKNOWN":  must(r.Unknown(allPass, core(t, "PLAN_INVALID"))),
		"WAIT":     must(r.Wait(allPass, 1_700_000_500, core(t, "RATE_LIMITED"))),
		"CLARIFY":  must(unresolved.Clarify(decide.Clarification{}, core(t, "AMBIGUOUS_COUNTERPARTY"))),
	}
	for name, d := range ds {
		got, err := d.WithBasis(minimalBasis())
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got.Basis().PackVersion != "fin.pack@1.0.0" || got.Basis().EvaluatedAt != 1_700_000_100 {
			t.Errorf("%s: %+v", name, got.Basis())
		}
	}
}

// REQ-AMB-001 / INV-06 acceptance, as a property over generated intents (testing/quick, fixed seed): an intent that is not
// fully resolved never yields a decision other than CLARIFY or ESCALATE, whatever basis is attached.
func TestAmbiguityNeverYieldsAnotherOutcomeWithAnyBasis(t *testing.T) {
	statuses := []decide.SlotStatus{decide.SlotResolved, decide.SlotUnresolved, decide.SlotAmbiguous, decide.SlotAbsent}
	ambiguous := 0
	property := func(flagCount uint8, slotStatuses [4]uint8) bool {
		v := decide.IntentView{Version: 5, RequiredSlots: map[string]decide.SlotStatus{}}
		for i, s := range slotStatuses {
			v.RequiredSlots[string(rune('a'+i))] = statuses[int(s)%len(statuses)]
		}
		for i := 0; i < int(flagCount)%2; i++ {
			v.MaterialFlags = append(v.MaterialFlags, "AMBIGUOUS_COUNTERPARTY")
		}
		adm := decide.Admit(v)
		if _, ok := adm.Resolved(); ok {
			return true
		}
		ambiguous++
		u, _ := adm.Unresolved()
		for _, build := range []func() (decide.Decision, error){
			func() (decide.Decision, error) {
				return u.Clarify(decide.Clarification{}, core(t, "AMBIGUOUS_COUNTERPARTY"))
			},
			func() (decide.Decision, error) { return u.Escalate(core(t, "AMBIGUOUS_COUNTERPARTY")) },
		} {
			d, err := build()
			if err != nil {
				return false
			}
			d, err = d.WithBasis(fullBasis())
			if err != nil || (d.Outcome() != decide.Clarify && d.Outcome() != decide.Escalate) {
				return false
			}
		}
		return true
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 5000, Rand: rand.New(rand.NewSource(20261103))}); err != nil {
		t.Fatal(err)
	}
	if ambiguous < 2000 {
		t.Fatalf("generator too narrow: %d ambiguous intents", ambiguous)
	}
}
