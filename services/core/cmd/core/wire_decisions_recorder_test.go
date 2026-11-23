package main

// STM-01 decisions lane, the Recorder (A537 interim, relayed, recorded in docs#229; A475, A503, A512; REQ-TRU-008, REQ-TRU-009, REQ-TRU-010; INV-03, INV-17, INV-29, AGENT-16,
// A330, A469, A486). The Decision Record writer is trustport.NewRecorderAdapter over the three lifetimes of loadRecorderSettings (grant at most record), the escalation set of the
// pack's escalation graph, the grant keys, and the seams no adapter on main fills (the Record write login, the sealed-facts source and the core-trust signing keys). A setting or an
// input that is absent is a gap named in fixed text; a present value that is invalid is fatal and names the setting only; the grant lifetime above the record lifetime is fatal.
// The DeclinedRequestID mapping is noema#892's and is not built here. Every value is SYNTHETIC.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
		{"grant lifetime", func(in *decisionInputs) { in.Env = decEnv(t, map[string]string{EnvTrustGrantLifetimeMS: "<unset>"}) }, EnvTrustGrantLifetimeMS},
		{"record lifetime", func(in *decisionInputs) { in.Env = decEnv(t, map[string]string{EnvTrustRecordLifetimeMS: "<unset>"}) }, EnvTrustRecordLifetimeMS},
		{"transaction timeout", func(in *decisionInputs) { in.Env = decEnv(t, map[string]string{EnvTrustTxTimeoutMS: "<unset>"}) }, EnvTrustTxTimeoutMS},
		{"escalation graph", func(in *decisionInputs) { in.Env = decEnv(t, map[string]string{EnvTrustEscalationGraph: "<unset>"}) }, EnvTrustEscalationGraph},
		{"pack", func(in *decisionInputs) { in.Pack = nil }, needPack},
		{"grant keys", func(in *decisionInputs) { in.Keys = nil }, needGrantKeys},
		{"record login", func(in *decisionInputs) { in.Begin = nil }, needRecordLogin},
		{"sealed facts", func(in *decisionInputs) { in.Facts = nil }, needSealedFacts},
		{"signing key directory", func(in *decisionInputs) { in.Env = decEnv(t, map[string]string{EnvTrustKeyDir: "<unset>"}) }, EnvTrustKeyDir},
		{"signing key overlap", func(in *decisionInputs) { in.Env = decEnv(t, map[string]string{EnvTrustKeyMaxActive: "<unset>"}) }, EnvTrustKeyMaxActive},
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
	in := decisionInputs{Env: decEnv(t, map[string]string{EnvTrustGrantLifetimeMS: "<unset>", EnvTrustRecordLifetimeMS: "<unset>", EnvTrustTxTimeoutMS: "<unset>", EnvTrustEscalationGraph: "<unset>", EnvTrustKeyDir: "<unset>", EnvTrustKeyMaxActive: "<unset>"})}
	p, err := buildDecisions(in)
	if err != nil {
		t.Fatalf("buildDecisions: %v", err)
	}
	g, _ := decGap(p, "Recorder")
	want := []string{EnvTrustGrantLifetimeMS, EnvTrustRecordLifetimeMS, EnvTrustTxTimeoutMS, EnvTrustEscalationGraph, needPack, needGrantKeys, needRecordLogin, needSealedFacts, EnvTrustKeyDir, EnvTrustKeyMaxActive}
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

// A548: the core-trust signing keys are read through the shared key loader. A present invalid value is fatal and names the setting and never the value or the path.
func TestAnInvalidKeyOverlapIsFatalAndNamesOnlyTheSetting(t *testing.T) {
	for _, bad := range []string{"0", "-1", "abc", "1.5", " 2"} {
		in := decInputs(t, map[string]string{EnvTrustKeyMaxActive: bad})
		in.Keys = nil // another input missing: the invalid value is still fatal
		p, err := buildDecisions(in)
		if err == nil || !strings.Contains(err.Error(), EnvTrustKeyMaxActive) || strings.Contains(err.Error(), bad) {
			t.Errorf("%q: error %v; want one that names %s and never the value", bad, err, EnvTrustKeyMaxActive)
		}
		if p.Recorder != nil {
			t.Errorf("%q: a Recorder was built beside the error", bad)
		}
	}
}

func TestAKeyDirectoryThatCannotBeLoadedIsFatalAndNeverEchoesThePath(t *testing.T) {
	cases := map[string]func(dir string){
		"no key": func(dir string) { _ = os.Remove(filepath.Join(dir, "core-trust.ed25519.1.key")) },
		"a key of another one": func(dir string) {
			_ = os.Rename(filepath.Join(dir, "core-trust.ed25519.1.key"), filepath.Join(dir, "core-edge.ed25519.1.key"))
		},
		"a group-readable key":  func(dir string) { _ = os.Chmod(filepath.Join(dir, "core-trust.ed25519.1.key"), 0o440) },
		"keys over the overlap": func(dir string) { writeKey(t, dir, "core-trust.ed25519.2.key", strings.Repeat("d4", 32), 0o400) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := decInputs(t, map[string]string{EnvTrustKeyMaxActive: "1"})
			dir := in.Env.Getenv(EnvTrustKeyDir)
			mutate(dir)
			in.Facts = nil // another input missing: the unusable keys are still fatal
			p, err := buildDecisions(in)
			if err == nil || !strings.Contains(err.Error(), EnvTrustKeyDir) || strings.Contains(err.Error(), dir) {
				t.Errorf("error %v; want one that names %s and never the path", err, EnvTrustKeyDir)
			}
			if p.Recorder != nil {
				t.Errorf("a Recorder was built beside the error")
			}
		})
	}
}

// The Record signer is the core-trust ring and no other identity's: the Appender holds one signer, under the identity of the Record writer (migrations 0012, 0019: the database checks
// the signature under an ACTIVE key of the written_by identity).
func TestTheRecordersAppenderSignsAsCoreTrustAndOnlyAsCoreTrust(t *testing.T) {
	cfg, missing, err := decisionRecorderConfig(decInputs(t, nil))
	if err != nil || len(missing) > 0 {
		t.Fatalf("config: %v, %v", err, missing)
	}
	if cfg.Appender == nil || len(cfg.Appender.Signers) != 1 || cfg.Appender.Signers["core-trust"] == nil {
		t.Fatalf("appender signers %+v; want exactly core-trust", cfg.Appender)
	}
	keyID, sig, _, err := cfg.Appender.Signers["core-trust"].SignRecordEvent(map[string]any{"synthetic": "body"})
	if err != nil || keyID != "core-trust.ed25519.1" || sig == "" {
		t.Errorf("sign: %q, %q, %v; want a signature under core-trust.ed25519.1", keyID, sig, err)
	}
	if cfg.Appender.Clock != cfg.Clock || cfg.Appender.IDs == nil {
		t.Errorf("the Appender does not share the lane's clock and ID source")
	}
}
