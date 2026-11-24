package trustpack_test

// STM-01 composition (A550, A551, interim, relayed, not yet committed): the two read-only accessors the composition needs of a verified Pack, each from the verified bytes alone, each
// returning a copy, each free of industry terms: ConfirmationRequirements (what the pack's CONFIRM rules ask of the approver, by capability) and Registry (the capability registry the
// Planner needs, built again from the verified bytes). A Pack that failed verification does not exist, so it yields none. REQ-TRU-009, REQ-STM-001; INV-14, INV-16, INV-21. SYNTHETIC.

import (
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
)

// accExpected reads the CONFIRM rules of the pack file independently: capability to approver requirement.
func accExpected(t testing.TB, raw []byte) map[string]string {
	t.Helper()
	var f struct {
		Policy struct {
			Rules []struct {
				Effect    string         `json:"effect"`
				AppliesTo []string       `json:"applies_to"`
				Params    map[string]any `json:"params"`
			} `json:"rules"`
		} `json:"policy"`
	}
	if err := jsonUnmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, r := range f.Policy.Rules {
		if r.Effect != "CONFIRM" {
			continue
		}
		req, _ := r.Params["approver_requirement"].(string)
		for _, c := range r.AppliesTo {
			out[c] = req
		}
	}
	return out
}

func TestConfirmationRequirementsAreWhatThePacksConfirmRulesAskOfTheApprover(t *testing.T) {
	want := accExpected(t, readPackFile(t, packFileName))
	if len(want) == 0 {
		t.Fatal("the real pack has no CONFIRM rule: the test would prove nothing")
	}
	got, err := loadReal(t).ConfirmationRequirements()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("ConfirmationRequirements = %v, %v; want %v", got, err, want)
	}
}

func TestConfirmationRequirementsAreACopy(t *testing.T) {
	p := loadReal(t)
	first, err := p.ConfirmationRequirements()
	if err != nil || len(first) == 0 {
		t.Fatalf("got %v, %v", first, err)
	}
	for c := range first {
		first[c] = "ROLE:tampered"
		delete(first, c)
	}
	second, err := p.ConfirmationRequirements()
	if err != nil || !reflect.DeepEqual(second, accExpected(t, readPackFile(t, packFileName))) {
		t.Fatalf("a caller's edit reached the pack: %v, %v", second, err)
	}
}

// A CONFIRM rule that asks for no usable approver, or two rules that ask different things of one capability, give no answer at all: the accessor never picks one (INV-16).
func TestConfirmationRequirementsRefuseWhatTheyCannotReadSoundly(t *testing.T) {
	cases := map[string]func(rule map[string]any){
		"no requirement":   func(r map[string]any) { delete(r, "params") },
		"unknown approver": func(r map[string]any) { r["params"] = map[string]any{"approver_requirement": "ANY"} },
		"an empty role":    func(r map[string]any) { r["params"] = map[string]any{"approver_requirement": "ROLE:"} },
		"a number":         func(r map[string]any) { r["params"] = map[string]any{"approver_requirement": 3} },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			m := files(t)
			m[packFileName] = edit(t, m[packFileName], func(p map[string]any) {
				for _, r := range p["policy"].(map[string]any)["rules"].([]any) {
					if rule := r.(map[string]any); rule["effect"] == "CONFIRM" {
						change(rule)
					}
				}
			})
			got, err := mustLoadFiles(t, m).ConfirmationRequirements()
			if !errors.Is(err, trustpack.ErrPack) || got != nil {
				t.Fatalf("got %v, %v; want ErrPack and no map", got, err)
			}
		})
	}
	t.Run("two rules, two requirements", func(t *testing.T) {
		m := files(t)
		m[packFileName] = edit(t, m[packFileName], func(p map[string]any) {
			rules := p["policy"].(map[string]any)["rules"].([]any)
			for _, r := range rules {
				if rule := r.(map[string]any); rule["effect"] == "CONFIRM" {
					twin := map[string]any{}
					for k, v := range rule {
						twin[k] = v
					}
					twin["rule_id"] = "synthetic.twin"
					twin["params"] = map[string]any{"approver_requirement": "MAKER_CHECKER"}
					p["policy"].(map[string]any)["rules"] = append(rules, twin)
					return
				}
			}
		})
		if got, err := mustLoadFiles(t, m).ConfirmationRequirements(); !errors.Is(err, trustpack.ErrPack) || got != nil {
			t.Fatalf("got %v, %v; want ErrPack and no map", got, err)
		}
	})
}

func TestARegistryIsBuiltFromTheVerifiedBytesAndIsACopyEachTime(t *testing.T) {
	pack, graphs := verifiedParts(t)
	p, err := trustpack.LoadVerified(pack, graphs)
	if err != nil {
		t.Fatal(err)
	}
	a, err := p.Registry()
	if err != nil || a == nil {
		t.Fatalf("Registry = %v, %v", a, err)
	}
	b, err := p.Registry()
	if err != nil || b == a {
		t.Fatalf("a second Registry is the same value (%p, %p, %v): a caller could change what another holds", a, b, err)
	}
	raw := readRawPack(t)
	for _, c := range raw.Caps {
		if got := a.IsProhibited(c.ID); got != c.Prohibited {
			t.Errorf("capability %s prohibited is %v in the registry, %v in the pack", c.ID, got, c.Prohibited)
		}
		if got := a.Registered(c.ID); got == c.Prohibited {
			t.Errorf("capability %s registered is %v in the registry; the pack says prohibited is %v (a prohibited capability is never registered as callable)", c.ID, got, c.Prohibited)
		}
	}
	if a.Registered("synthetic.cap.not_in_the_pack") {
		t.Error("a capability the pack does not have is registered")
	}
}

// A551: a pack that fails verification is not a Pack, so no accessor answers; a nil or zero Pack gives none either.
func TestAPackThatFailsVerificationYieldsNoAccessorAnswer(t *testing.T) {
	pack, graphs := verifiedParts(t)
	for name, tc := range map[string]struct {
		pack   []byte
		graphs map[string][]byte
	}{
		"no bytes":          {nil, graphs},
		"a graph is absent": {pack, map[string][]byte{}},
		"a bad document":    {[]byte(`{"pack_id":"x"}`), graphs},
	} {
		t.Run(name, func(t *testing.T) {
			p, err := trustpack.LoadVerified(tc.pack, tc.graphs)
			if err == nil || p != nil {
				t.Fatalf("LoadVerified = %v, %v; want an error and no Pack", p, err)
			}
			if m, aerr := p.ConfirmationRequirements(); !errors.Is(aerr, trustpack.ErrConfig) || m != nil {
				t.Errorf("ConfirmationRequirements of the failed pack = %v, %v; want ErrConfig", m, aerr)
			}
			if r, aerr := p.Registry(); !errors.Is(aerr, trustpack.ErrConfig) || r != nil {
				t.Errorf("Registry of the failed pack = %v, %v; want ErrConfig", r, aerr)
			}
		})
	}
	var zero trustpack.Pack
	if r, err := zero.Registry(); !errors.Is(err, trustpack.ErrConfig) || r != nil {
		t.Errorf("zero Pack Registry = %v, %v; want ErrConfig", r, err)
	}
}

// INV-14: the accessors name no id of the pack (its prefix), so no capability, node or policy of any industry.
func TestTheAccessorsNameNoIdOfThePack(t *testing.T) {
	src, err := os.ReadFile("accessors.go")
	if err != nil {
		t.Fatal(err)
	}
	if lits := scanSource(t, string(src), packPrefixes(t)); len(lits) != 0 {
		t.Errorf("accessors.go names ids of the pack: %v", lits)
	}
}
