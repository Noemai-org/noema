package main

// STM-01 decisions lane, the Recorder (A537 interim, relayed, recorded in docs#229; A475, A503, A512; REQ-TRU-008, REQ-TRU-009, REQ-TRU-010; INV-03, INV-17, INV-29, AGENT-16,
// A330, A469, A486). The Decision Record writer is trustport.NewRecorderAdapter over the three lifetimes of loadRecorderSettings (grant at most record), the escalation set of the
// pack's escalation graph, the grant keys, and the seams no adapter on main fills (the Record write login, the sealed-facts source and the core-trust signing keys). A setting or an
// input that is absent is a gap named in fixed text; a present value that is invalid is fatal and names the setting only; the grant lifetime above the record lifetime is fatal.
// The DeclinedRequestID mapping is noema#892's and is not built here. Every value is SYNTHETIC.

import (
	"context"
	"errors"
	"strings"
	"testing"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

func TestTheRecorderIsTheTrustAdapterWhenEveryInputIsGiven(t *testing.T) {
	p, err := buildDecisions(decInputs(t, nil))
	if err != nil {
		t.Fatalf("buildDecisions: %v", err)
	}
	ra, ok := p.Recorder.(*trustport.RecorderAdapter)
	if !ok || ra == nil {
		t.Fatalf("Recorder is %T, want *trustport.RecorderAdapter", p.Recorder)
	}
	if g, gapped := decGap(p, "Recorder"); gapped {
		t.Errorf("the Recorder is built and still named as a gap: %v", g)
	}
	// The adapter's own refusal: a Record with no deadline writes nothing and opens no transaction (AGENTS section 7).
	if _, err := ra.Record(context.Background(), intake.RecordRequest{}); !errors.Is(err, trustport.ErrNoDeadline) {
		t.Errorf("Record without a deadline: %v; want ErrNoDeadline", err)
	}
}

func TestEachAbsentRecorderInputIsNamedAloneInFixedTextAndIsNotAnError(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*decisionInputs)
		missing string
	}{
		{"grant lifetime", func(in *decisionInputs) { in.Env = decEnv(map[string]string{EnvTrustGrantLifetimeMS: "<unset>"}) }, EnvTrustGrantLifetimeMS},
		{"record lifetime", func(in *decisionInputs) { in.Env = decEnv(map[string]string{EnvTrustRecordLifetimeMS: "<unset>"}) }, EnvTrustRecordLifetimeMS},
		{"transaction timeout", func(in *decisionInputs) { in.Env = decEnv(map[string]string{EnvTrustTxTimeoutMS: "<unset>"}) }, EnvTrustTxTimeoutMS},
		{"escalation graph", func(in *decisionInputs) { in.Env = decEnv(map[string]string{EnvTrustEscalationGraph: "<unset>"}) }, EnvTrustEscalationGraph},
		{"pack", func(in *decisionInputs) { in.Pack = nil }, needPack},
		{"grant keys", func(in *decisionInputs) { in.Keys = nil }, needGrantKeys},
		{"record login", func(in *decisionInputs) { in.Begin = nil }, needRecordLogin},
		{"sealed facts", func(in *decisionInputs) { in.Facts = nil }, needSealedFacts},
		{"signing keys", func(in *decisionInputs) { in.Appender = nil }, needRecordSigners},
		{"a typed nil is absent too", func(in *decisionInputs) { var f *typedNilFacts; in.Facts = f }, needSealedFacts},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := decInputs(t, nil)
			c.mutate(&in)
			p, err := buildDecisions(in)
			if err != nil {
				t.Fatalf("an absent input is a gap, not an error (A486): %v", err)
			}
			if p.Recorder != nil {
				t.Errorf("the Recorder is built from incomplete inputs: %T", p.Recorder)
			}
			g, ok := decGap(p, "Recorder")
			if !ok || strings.Join(g.Missing, "|") != c.missing {
				t.Errorf("gap %+v (named: %v); want Recorder lacking only %q", g, ok, c.missing)
			}
		})
	}
}

type typedNilFacts struct{}

func (*typedNilFacts) Facts(context.Context, intake.RecordRequest) (trustport.RecordFacts, error) {
	return trustport.RecordFacts{}, nil
}

func TestEveryAbsentRecorderInputIsNamedInTheFixedOrder(t *testing.T) {
	in := decisionInputs{Env: decEnv(map[string]string{EnvTrustGrantLifetimeMS: "<unset>", EnvTrustRecordLifetimeMS: "<unset>", EnvTrustTxTimeoutMS: "<unset>", EnvTrustEscalationGraph: "<unset>"})}
	p, err := buildDecisions(in)
	if err != nil {
		t.Fatalf("buildDecisions: %v", err)
	}
	g, _ := decGap(p, "Recorder")
	want := []string{EnvTrustGrantLifetimeMS, EnvTrustRecordLifetimeMS, EnvTrustTxTimeoutMS, EnvTrustEscalationGraph, needPack, needGrantKeys, needRecordLogin, needSealedFacts, needRecordSigners}
	if strings.Join(g.Missing, "|") != strings.Join(want, "|") {
		t.Errorf("missing %q; want %q", g.Missing, want)
	}
}

// A469: a present value that is invalid is fatal and names the setting only, whatever else is missing.
func TestAnInvalidRecorderSettingIsFatalAndNamesOnlyTheSetting(t *testing.T) {
	for _, name := range []string{EnvTrustGrantLifetimeMS, EnvTrustRecordLifetimeMS, EnvTrustTxTimeoutMS} {
		for _, bad := range []string{"0", "-5", "abc", "1.5", " 5", "007", "99999999999999999999"} {
			in := decInputs(t, map[string]string{name: bad})
			in.Keys, in.Facts = nil, nil // other inputs missing: the invalid value is still fatal
			p, err := buildDecisions(in)
			if err == nil || !strings.Contains(err.Error(), name) || strings.Contains(err.Error(), bad) {
				t.Errorf("%s=%q: error %v; want one that names the setting and never the value", name, bad, err)
			}
			if p.Recorder != nil {
				t.Errorf("%s=%q: a Recorder was built beside the error", name, bad)
			}
		}
	}
}

// A503: a grant must not outlive its record. Equal is accepted.
func TestAGrantLifetimeAboveTheRecordLifetimeIsFatalAndEqualIsAccepted(t *testing.T) {
	in := decInputs(t, map[string]string{EnvTrustGrantLifetimeMS: "9000", EnvTrustRecordLifetimeMS: "8000"})
	_, err := buildDecisions(in)
	if err == nil || !strings.Contains(err.Error(), EnvTrustGrantLifetimeMS) || !strings.Contains(err.Error(), EnvTrustRecordLifetimeMS) || strings.Contains(err.Error(), "9000") {
		t.Errorf("grant above record: %v; want an error that names both settings and no value", err)
	}
	p, err := buildDecisions(decInputs(t, map[string]string{EnvTrustGrantLifetimeMS: "8000", EnvTrustRecordLifetimeMS: "8000"}))
	if err != nil || p.Recorder == nil {
		t.Errorf("equal lifetimes: %v, %T; want a Recorder", err, p.Recorder)
	}
}

// INV-16: an escalation graph the pack does not hold is a startup error, never a gap and never an empty set (a decision with an escalation step would then carry no disposition).
func TestAnEscalationGraphThePackLacksIsFatalAndNotAnEmptySet(t *testing.T) {
	in := decInputs(t, map[string]string{EnvTrustEscalationGraph: "no.such.graph"})
	p, err := buildDecisions(in)
	if !errors.Is(err, trustpack.ErrEscalationGraph) {
		t.Errorf("error %v; want ErrEscalationGraph", err)
	}
	if p.Recorder != nil {
		t.Errorf("a Recorder was built with no escalation set: %T", p.Recorder)
	}
	if err != nil && strings.Contains(err.Error(), "no.such.graph") {
		t.Errorf("the error repeats the setting's value: %v", err)
	}
}

func TestNoRecorderSettingHasADefaultInCode(t *testing.T) {
	p, err := buildDecisions(decisionInputs{Env: Env{Getenv: func(string) string { return "" }}})
	if err != nil {
		t.Fatalf("an empty environment is a set of gaps, not an error: %v", err)
	}
	if p.Recorder != nil || p.Intents != nil {
		t.Errorf("an adapter was built from nothing: %T, %T", p.Recorder, p.Intents)
	}
}
