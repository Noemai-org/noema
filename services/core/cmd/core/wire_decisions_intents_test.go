package main

// STM-01 decisions lane, Intents (A537 interim, relayed, recorded in docs#229; REQ-TRU-003, REQ-SEC-004; INV-12, INV-16, AGENT-16, A330, A469, A486). The admitted-intent store
// adapter is built over the verified pack's bytes and the tenant's own Trust login (trustport.NewStoredIntents), with the read bound of NOEMA_TRUST_DB_TIMEOUT_MS. An input or a
// setting that is absent is a gap named in fixed text and never an error; a present value that is invalid, or pack bytes the adapter cannot trust, stop the process and name the
// setting only. Every value is SYNTHETIC.

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
)

func TestIntentsAreTheStoredIntentsAdapterAndReadOnlyAsTheTenantsOwnLogin(t *testing.T) {
	p, err := buildDecisions(decInputs(t, nil))
	if err != nil {
		t.Fatalf("buildDecisions: %v", err)
	}
	si, ok := p.Intents.(*trustport.StoredIntents)
	if !ok || si == nil {
		t.Fatalf("Intents is %T, want *trustport.StoredIntents", p.Intents)
	}
	if g, gapped := decGap(p, "Intents"); gapped {
		t.Errorf("Intents is built and still named as a gap: %v", g)
	}
	// INV-12: a tenant with no login of its own gets no one else's: the adapter is asked, and the answer is the tenant-bound refusal.
	_, err = p.Intents.Get(t.Context(), "ten_zz", "int_"+strings.Repeat("a", 26), 1)
	if !errors.Is(err, trustport.ErrNoPool) {
		t.Errorf("Get for a tenant with no login: %v; want ErrNoPool", err)
	}
}

func TestAnIntentsInputThatIsAbsentIsAGapNamedInFixedTextAndNoError(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*decisionInputs)
		missing []string
	}{
		{"no pack", func(in *decisionInputs) { in.Pack = nil }, []string{needPack}},
		{"no logins", func(in *decisionInputs) { in.Pools = nil }, []string{needTrustLogins}},
		{"no read bound", func(in *decisionInputs) { in.Env = decEnv(t, map[string]string{EnvTrustDBTimeout: "<unset>"}) }, []string{EnvTrustDBTimeout}},
		{"all three, in the fixed order", func(in *decisionInputs) {
			in.Pack, in.Pools = nil, nil
			in.Env = decEnv(t, map[string]string{EnvTrustDBTimeout: "<unset>"})
		}, []string{needTrustLogins, needPack, EnvTrustDBTimeout}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := decInputs(t, nil)
			c.mutate(&in)
			p, err := buildDecisions(in)
			if err != nil {
				t.Fatalf("a missing input is a gap, not an error (A486): %v", err)
			}
			if p.Intents != nil {
				t.Errorf("Intents is built from incomplete inputs: %T", p.Intents)
			}
			g, ok := decGap(p, "Intents")
			if !ok || strings.Join(g.Missing, "|") != strings.Join(c.missing, "|") {
				t.Errorf("gap %+v (named: %v); want Intents lacking %v", g, ok, c.missing)
			}
		})
	}
}

func TestAnInvalidIntentsReadBoundIsFatalAndNamesTheSettingNotTheValue(t *testing.T) {
	for _, bad := range []string{"0", "-1", "abc", "1.5", " 5", "007", "99999999999999999999"} {
		in := decInputs(t, map[string]string{EnvTrustDBTimeout: bad})
		p, err := buildDecisions(in)
		if err == nil {
			t.Errorf("%q: no error", bad)
			continue
		}
		if !strings.Contains(err.Error(), EnvTrustDBTimeout) || strings.Contains(err.Error(), bad) {
			t.Errorf("%q: error %q must name %s and never the value", bad, err, EnvTrustDBTimeout)
		}
		if p.Intents != nil {
			t.Errorf("%q: an adapter was built beside the error", bad)
		}
	}
}

// INV-16: a Pack the adapter cannot trust (a zero Pack holds no pack document) is a startup error, never a named gap.
func TestAPackTheAdapterCannotTrustIsFatalAndNotAGap(t *testing.T) {
	in := decInputs(t, nil)
	in.Pack = &trustpack.Pack{}
	p, err := buildDecisions(in)
	if !errors.Is(err, trustport.ErrPackData) {
		t.Errorf("error %v; want ErrPackData", err)
	}
	if _, gapped := decGap(p, "Intents"); gapped || p.Intents != nil {
		t.Errorf("the bad pack was turned into a gap or an adapter")
	}
}

// One source (A548, INV-16): the lane takes the pack once, as a Pack. There is no second field that carries pack bytes, so a copy that disagrees with the Pack cannot be supplied.
func TestThereIsOneSourceOfThePackAndNoSecondCopyOfItsBytes(t *testing.T) {
	typ := reflect.TypeOf(decisionInputs{})
	for _, gone := range []string{"PackJSON", "Appender"} {
		if _, has := typ.FieldByName(gone); has {
			t.Errorf("decisionInputs still has the field %s", gone)
		}
	}
	si, _ := buildDecisions(decInputs(t, nil))
	if si.Intents == nil {
		t.Fatal("no Intents from a Pack alone")
	}
}
