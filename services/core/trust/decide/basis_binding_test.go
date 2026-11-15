package decide_test

// TRU-13 (REQ-TRU-001, REQ-TRU-004, REQ-TRU-007; INV-09): the basis of a decision (pack version, the policy version of each rule that decided, the
// evidence snapshot and the freshness of each evidence item) is built by the gate steps from what T9 and T11 actually read and evaluated, never
// supplied by the caller, and a decision whose basis differs from the evaluated set is refused at recording. All data is SYNTHETIC.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

func freshness(source string, age, limit int64) decide.Freshness {
	return decide.Freshness{EvidenceID: "ev_" + source, EffectiveAt: 1_700_000_000, Source: source, AgeSeconds: age, EffectiveLimitSeconds: limit, LimitSetBy: decide.LimitByTrustProfile}
}

func rule(id, policy, version string) decide.PolicyRuleVersion {
	return decide.PolicyRuleVersion{RuleID: id, PolicyID: policy, Version: version}
}

func recorded(t testing.TB) *decide.BasisBuilder {
	t.Helper()
	b, err := decide.NewBasisBuilder("fin.pack@1.0.0", 1_700_000_100)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.RecordEvidence("snap_01", []decide.Freshness{freshness("fin.src.ledger", 42, 300), freshness("fin.src.switch", 7, 120)}); err != nil {
		t.Fatal(err)
	}
	if err := b.RecordPolicy([]decide.PolicyRuleVersion{rule("fin.pol.default.permit_reads", "fin.pol", "3.1"), rule("fin.pol.admission.dedupe", "fin.pol", "3.0")}); err != nil {
		t.Fatal(err)
	}
	return b
}

// The freshness of an evidence item carries what the Decision Record contract requires: the source, the age, the effective limit and who set it.
func TestAFreshnessItemMustCarryTheFieldsTheRecordRequires(t *testing.T) {
	good := freshness("fin.src.ledger", 42, 300)
	for name, mutate := range map[string]func(*decide.Freshness){
		"no source":                 func(f *decide.Freshness) { f.Source = "" },
		"a negative age":            func(f *decide.Freshness) { f.AgeSeconds = -1 },
		"a zero limit":              func(f *decide.Freshness) { f.EffectiveLimitSeconds = 0 },
		"a negative limit":          func(f *decide.Freshness) { f.EffectiveLimitSeconds = -5 },
		"no limit_set_by":           func(f *decide.Freshness) { f.LimitSetBy = "" },
		"an unknown limit_set_by":   func(f *decide.Freshness) { f.LimitSetBy = "SOMEONE" },
		"a lower-case limit_set_by": func(f *decide.Freshness) { f.LimitSetBy = "source" },
	} {
		f := good
		mutate(&f)
		b := fullBasis()
		b.EvidenceFreshness = []decide.Freshness{f}
		d := must(resolvedIntent(t).Allow(allPass))
		if _, err := d.WithBasis(b); !errors.Is(err, decide.ErrBasisInvalid) {
			t.Errorf("%s: err %v, want ErrBasisInvalid", name, err)
		}
	}
	b := fullBasis()
	b.EvidenceFreshness = []decide.Freshness{good}
	if _, err := must(resolvedIntent(t).Allow(allPass)).WithBasis(b); err != nil {
		t.Fatalf("a complete freshness item: %v", err)
	}
}

func TestTheBasisDigestIsStableOrderIndependentAndCoversEveryField(t *testing.T) {
	base := recorded(t)
	d1, err := base.Digest()
	if err != nil || !strings.HasPrefix(d1, "sha256:") || len(d1) != len("sha256:")+64 {
		t.Fatalf("digest %q %v", d1, err)
	}
	// the same set recorded in the other order is the same set
	rev, _ := decide.NewBasisBuilder("fin.pack@1.0.0", 1_700_000_100)
	_ = rev.RecordEvidence("snap_01", []decide.Freshness{freshness("fin.src.switch", 7, 120), freshness("fin.src.ledger", 42, 300)})
	_ = rev.RecordPolicy([]decide.PolicyRuleVersion{rule("fin.pol.admission.dedupe", "fin.pol", "3.0"), rule("fin.pol.default.permit_reads", "fin.pol", "3.1")})
	if d2, _ := rev.Digest(); d2 != d1 {
		t.Fatalf("the order of the evaluated set changed the digest: %s %s", d1, d2)
	}
	// any single change is a different digest
	changed := map[string]decide.Basis{}
	mk := func(name string, f func(*decide.Basis)) {
		b, err := base.Basis()
		if err != nil {
			t.Fatal(err)
		}
		f(&b)
		changed[name] = b
	}
	mk("pack", func(b *decide.Basis) { b.PackVersion = "fin.pack@1.0.1" })
	mk("snapshot", func(b *decide.Basis) { b.EvidenceSnapshotID = "snap_02" })
	mk("time", func(b *decide.Basis) { b.EvaluatedAt++ })
	mk("a rule version", func(b *decide.Basis) { b.PolicyRules[0].Version = "3.2" })
	mk("a rule id", func(b *decide.Basis) { b.PolicyRules[0].RuleID = "fin.pol.other" })
	mk("a policy id", func(b *decide.Basis) { b.PolicyRules[0].PolicyID = "fin.other" })
	mk("a rule dropped", func(b *decide.Basis) { b.PolicyRules = b.PolicyRules[:1] })
	mk("a rule added", func(b *decide.Basis) { b.PolicyRules = append(b.PolicyRules, rule("x", "fin.pol", "1.0")) })
	mk("an evidence id", func(b *decide.Basis) { b.EvidenceFreshness[0].EvidenceID = "ev_other" })
	mk("a source", func(b *decide.Basis) { b.EvidenceFreshness[0].Source = "fin.src.other" })
	mk("an age", func(b *decide.Basis) { b.EvidenceFreshness[0].AgeSeconds++ })
	mk("a limit", func(b *decide.Basis) { b.EvidenceFreshness[0].EffectiveLimitSeconds++ })
	mk("limit_set_by", func(b *decide.Basis) { b.EvidenceFreshness[0].LimitSetBy = decide.LimitBySource })
	mk("an effective time", func(b *decide.Basis) { b.EvidenceFreshness[0].EffectiveAt++ })
	mk("an item dropped", func(b *decide.Basis) { b.EvidenceFreshness = b.EvidenceFreshness[:1] })
	for name, b := range changed {
		got, err := b.Digest()
		if err != nil || got == d1 {
			t.Errorf("%s did not change the digest (%v)", name, err)
		}
	}
	// two different fields cannot be confused by moving a boundary between them
	a := decide.Basis{PackVersion: "ab", EvidenceSnapshotID: "c", EvaluatedAt: 1}
	b := decide.Basis{PackVersion: "a", EvidenceSnapshotID: "bc", EvaluatedAt: 1}
	da, _ := a.Digest()
	db, _ := b.Digest()
	if da == db {
		t.Fatal("moving a character between two fields kept the digest: the encoding is not length-prefixed")
	}
}

// The steps record what they read and evaluated; the decision's basis comes from that record. A decision cannot bring a basis of its own.
func TestTheBasisIsBuiltFromWhatTheStepsRecordedAndNotSuppliedByTheCaller(t *testing.T) {
	b := recorded(t)
	allow := must(resolvedIntent(t).Allow(allPass))
	got, err := b.Attach(allow)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := b.Basis()
	if !got.HasCompleteBasis() || fmt.Sprint(got.Basis()) != fmt.Sprint(want) {
		t.Fatalf("the attached basis is not the evaluated set:\n%+v\n%+v", got.Basis(), want)
	}
	if err := b.Check(got); err != nil {
		t.Fatalf("the evaluated decision is refused at recording: %v", err)
	}
	// a decision that already carries a basis of its own (the caller supplied one) is refused
	own, err := allow.WithBasis(fullBasis())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Attach(own); !errors.Is(err, decide.ErrBasisInvalid) {
		t.Fatalf("a decision with a caller-supplied basis was accepted: %v", err)
	}
	// the evaluated set cannot be recorded twice: a later step cannot rewrite what an earlier one read
	if err := b.RecordEvidence("snap_99", []decide.Freshness{freshness("fin.src.ledger", 1, 300)}); !errors.Is(err, decide.ErrBasisRecorded) {
		t.Fatalf("evidence recorded twice: %v", err)
	}
	if err := b.RecordPolicy([]decide.PolicyRuleVersion{rule("x", "fin.pol", "9.9")}); !errors.Is(err, decide.ErrBasisRecorded) {
		t.Fatalf("policy recorded twice: %v", err)
	}
}

func TestAnAllowOrConfirmNeedsBothStepsToHaveRecordedTheirSet(t *testing.T) {
	for name, build := range map[string]func() *decide.BasisBuilder{
		"nothing recorded": func() *decide.BasisBuilder { b, _ := decide.NewBasisBuilder("fin.pack@1.0.0", 1); return b },
		"only the evidence (T9)": func() *decide.BasisBuilder {
			b, _ := decide.NewBasisBuilder("fin.pack@1.0.0", 1)
			_ = b.RecordEvidence("snap_01", []decide.Freshness{freshness("fin.src.ledger", 1, 300)})
			return b
		},
		"only the policy (T11)": func() *decide.BasisBuilder {
			b, _ := decide.NewBasisBuilder("fin.pack@1.0.0", 1)
			_ = b.RecordPolicy([]decide.PolicyRuleVersion{rule("r", "fin.pol", "1.0")})
			return b
		},
		"an empty evidence set": func() *decide.BasisBuilder {
			b, _ := decide.NewBasisBuilder("fin.pack@1.0.0", 1)
			_ = b.RecordEvidence("snap_01", nil)
			_ = b.RecordPolicy([]decide.PolicyRuleVersion{rule("r", "fin.pol", "1.0")})
			return b
		},
		"an empty policy set": func() *decide.BasisBuilder {
			b, _ := decide.NewBasisBuilder("fin.pack@1.0.0", 1)
			_ = b.RecordEvidence("snap_01", []decide.Freshness{freshness("fin.src.ledger", 1, 300)})
			_ = b.RecordPolicy(nil)
			return b
		},
	} {
		for _, d := range []decide.Decision{must(resolvedIntent(t).Allow(allPass)), must(resolvedIntent(t).Confirm(allPass))} {
			if got, err := build().Attach(d); !errors.Is(err, decide.ErrBasisIncomplete) {
				t.Errorf("%s: %v (decision %+v)", name, err, got.Outcome())
			}
		}
	}
	// a refusal before any evidence needs only the pack version and the time (T1 to T6)
	b, _ := decide.NewBasisBuilder("fin.pack@1.0.0", 1)
	refusal, err := decide.FailAtGate(decide.T4Capability, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := b.Attach(refusal); err != nil || got.Basis().PackVersion != "fin.pack@1.0.0" || len(got.Basis().PolicyRules) != 0 {
		t.Fatalf("a refusal before any read: %v %+v", err, got.Basis())
	}
}

// TRU-13 done-when: a test that changes the basis between evaluation and recording is refused.
func TestAChangeToTheBasisBetweenEvaluationAndRecordingIsRefused(t *testing.T) {
	b := recorded(t)
	evaluated, _ := b.Basis()
	for name, mutate := range map[string]func(*decide.Basis){
		"a rule version raised": func(x *decide.Basis) { x.PolicyRules[0].Version = "9.9" },
		"a rule removed":        func(x *decide.Basis) { x.PolicyRules = x.PolicyRules[1:] },
		"a snapshot swapped":    func(x *decide.Basis) { x.EvidenceSnapshotID = "snap_other" },
		"an age made younger":   func(x *decide.Basis) { x.EvidenceFreshness[0].AgeSeconds = 0 },
		"a limit widened":       func(x *decide.Basis) { x.EvidenceFreshness[0].EffectiveLimitSeconds *= 10 },
		"the pack changed":      func(x *decide.Basis) { x.PackVersion = "fin.pack@2.0.0" },
		"the time moved":        func(x *decide.Basis) { x.EvaluatedAt += 60 },
	} {
		x := evaluated
		x.PolicyRules = append([]decide.PolicyRuleVersion(nil), evaluated.PolicyRules...)
		x.EvidenceFreshness = append([]decide.Freshness(nil), evaluated.EvidenceFreshness...)
		mutate(&x)
		swapped, err := must(resolvedIntent(t).Allow(allPass)).WithBasis(x) // what a caller would do instead of using the builder
		if err != nil {
			continue // not even a valid basis: refused earlier
		}
		if err := b.Check(swapped); !errors.Is(err, decide.ErrBasisDiffers) {
			t.Errorf("%s: err %v, want ErrBasisDiffers", name, err)
		}
	}
	if err := b.Check(must(resolvedIntent(t).Allow(allPass))); !errors.Is(err, decide.ErrBasisDiffers) {
		t.Errorf("a decision with no basis at all: %v", err)
	}
}

// Property (TRU-13 done-when): over random evaluated sets and random single changes, no ALLOW or CONFIRM whose basis is not the evaluated set passes Check,
// and the unchanged one always does.
func TestNoAllowOrConfirmPassesWhoseBasisIsNotTheEvaluatedSet(t *testing.T) {
	allowD, confirmD := must(resolvedIntent(t).Allow(allPass)), must(resolvedIntent(t).Confirm(allPass))
	rapid.Check(t, func(t *rapid.T) {
		nf := rapid.IntRange(1, 4).Draw(t, "evidence items")
		nr := rapid.IntRange(1, 4).Draw(t, "rules")
		b, _ := decide.NewBasisBuilder("fin.pack@1.0.0", rapid.Int64Range(1, 1<<40).Draw(t, "time"))
		var fs []decide.Freshness
		for i := 0; i < nf; i++ {
			f := freshness(fmt.Sprintf("fin.src.s%d", i), rapid.Int64Range(0, 3600).Draw(t, "age"), rapid.Int64Range(1, 3600).Draw(t, "limit"))
			f.EvidenceID = fmt.Sprintf("ev_%d", i)
			fs = append(fs, f)
		}
		var rs []decide.PolicyRuleVersion
		for i := 0; i < nr; i++ {
			rs = append(rs, rule(fmt.Sprintf("fin.pol.r%d", i), "fin.pol", fmt.Sprintf("%d.%d", rapid.IntRange(1, 9).Draw(t, "major"), rapid.IntRange(0, 9).Draw(t, "minor"))))
		}
		if err := b.RecordEvidence("snap_01", fs); err != nil {
			t.Fatal(err)
		}
		if err := b.RecordPolicy(rs); err != nil {
			t.Fatal(err)
		}
		evaluated, _ := b.Basis()
		outcome := rapid.SampledFrom([]string{"allow", "confirm"}).Draw(t, "outcome")
		mk := func(x decide.Basis) (decide.Decision, error) {
			if outcome == "allow" {
				return allowD.WithBasis(x)
			}
			return confirmD.WithBasis(x)
		}
		if d, err := mk(evaluated); err != nil || b.Check(d) != nil {
			t.Fatalf("the evaluated basis was refused: %v", err)
		}
		x := evaluated
		x.PolicyRules = append([]decide.PolicyRuleVersion(nil), evaluated.PolicyRules...)
		x.EvidenceFreshness = append([]decide.Freshness(nil), evaluated.EvidenceFreshness...)
		switch rapid.IntRange(0, 5).Draw(t, "which change") {
		case 0:
			x.PolicyRules[rapid.IntRange(0, nr-1).Draw(t, "rule")].Version += "1"
		case 1:
			x.EvidenceFreshness[rapid.IntRange(0, nf-1).Draw(t, "item")].AgeSeconds++
		case 2:
			x.EvidenceFreshness[rapid.IntRange(0, nf-1).Draw(t, "item")].EffectiveLimitSeconds++
		case 3:
			x.EvidenceSnapshotID += "x"
		case 4:
			x.EvaluatedAt++
		default:
			x.PackVersion += "x"
		}
		d, err := mk(x)
		if err != nil {
			return
		}
		if err := b.Check(d); !errors.Is(err, decide.ErrBasisDiffers) {
			t.Fatalf("a changed basis passed Check: %v", err)
		}
	})
}

// Codex P1 on noema#232: Evaluate alone accepts any complete basis a step supplies. EvaluateBound is the evaluation the Decision Record path uses: an ALLOW or
// CONFIRM stands only if its basis is exactly what the steps recorded (INV-16, REQ-TRU-001); otherwise the evaluation ends in UNKNOWN INTERNAL_ERROR.
func TestEvaluateBoundRefusesAnAllowWhoseBasisIsNotTheEvaluatedSet(t *testing.T) {
	stop := func(b *decide.BasisBuilder, d decide.Decision) decide.Steps {
		s := continueAll()
		s[decide.T15DecisionRecorded-1] = func(context.Context) decide.StepResult { return decide.Stop(d) }
		return s
	}
	b := recorded(t)
	genuine, err := b.Attach(must(resolvedIntent(t).Allow(allPass)))
	if err != nil {
		t.Fatal(err)
	}
	got, trace := decide.EvaluateBound(context.Background(), version, stop(b, genuine), b)
	if got.Outcome() != decide.Allow || trace[len(trace)-1].Result != decide.StepTerminal {
		t.Fatalf("the genuine ALLOW: %s %v", got.Outcome(), trace[len(trace)-1])
	}
	// a basis the caller made up: complete and valid, but not what the steps recorded
	fabricated, err := must(resolvedIntent(t).Allow(allPass)).WithBasis(fullBasis())
	if err != nil {
		t.Fatal(err)
	}
	for name, d := range map[string]decide.Decision{"a fabricated basis": fabricated, "another evaluation's set": otherSet(t)} {
		got, trace := decide.EvaluateBound(context.Background(), version, stop(b, d), b)
		if !isInternalError(got) || trace[len(trace)-1].Result != decide.StepInvalidResult {
			t.Errorf("%s: outcome %s, last trace %v", name, got.Outcome(), trace[len(trace)-1])
		}
	}
	// no builder means nothing to check the basis against: an ALLOW is not trusted
	if got, _ := decide.EvaluateBound(context.Background(), version, stop(b, genuine), nil); !isInternalError(got) {
		t.Errorf("no builder: outcome %s", got.Outcome())
	}
	// an outcome that carries no basis passes through unchanged
	rej := func(context.Context) decide.StepResult {
		return decide.Stop(must(resolvedIntent(t).Reject(allPass, core(t, "NO_PERMITTING_RULE"))))
	}
	s := continueAll()
	s[decide.T5Scope-1] = rej
	if got, _ := decide.EvaluateBound(context.Background(), version, s, b); got.Outcome() != decide.Reject {
		t.Errorf("a REJECT: %s", got.Outcome())
	}
}

// otherSet is a genuine builder-made basis of a different evaluation (another snapshot), as a decision carried over from elsewhere would have.
func otherSet(t *testing.T) decide.Decision {
	t.Helper()
	o, err := decide.NewBasisBuilder("fin.pack@1.0.0", 1_700_000_100)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.RecordEvidence("snap_99", []decide.Freshness{freshness("fin.src.ledger", 42, 300)}); err != nil {
		t.Fatal(err)
	}
	if err := o.RecordPolicy([]decide.PolicyRuleVersion{rule("fin.pol.default.permit_reads", "fin.pol", "3.1")}); err != nil {
		t.Fatal(err)
	}
	d, err := o.Attach(must(resolvedIntent(t).Allow(allPass)))
	if err != nil {
		t.Fatal(err)
	}
	return d
}
