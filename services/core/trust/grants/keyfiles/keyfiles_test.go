package keyfiles_test

// SEC-09 (SR), REQ-SEC-005, INV-03, INV-29: the grant key files. These tests run on every system; the ones that open real files are in read_unix_test.go. All keys are SYNTHETIC.

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/grants/keyfiles"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
)

var epoch = time.Date(2026, 11, 4, 16, 0, 0, 0, time.UTC)

func hexOf(c string) string { return strings.Repeat(c, 64) }

func mustParse(t *testing.T, name, content string) keyfiles.Parsed {
	t.Helper()
	p, err := keyfiles.Parse(name, []byte(content))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return p
}

// issue signs one synthetic grant under the ring and returns the key id the signature names.
func issue(t *testing.T, ring *grants.Keys) contracts.DecisionGrantV1 {
	t.Helper()
	r, _ := decide.Admit(decide.IntentView{Version: 2}).Resolved()
	d, err := r.Allow(decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass})
	if err != nil {
		t.Fatal(err)
	}
	d, err = d.WithBasis(decide.Basis{PackVersion: "fin.pack@1.0.0", PolicyRules: []decide.PolicyRuleVersion{{RuleID: "fin.pol.default.permit_case_writes", PolicyID: "fin.pol", Version: "3.1"}},
		EvidenceSnapshotID: "snap_01", EvidenceFreshness: []decide.Freshness{{EvidenceID: "ev_1", EffectiveAt: epoch.Unix() - 30, Source: "fin.src.ledger", AgeSeconds: 30, EffectiveLimitSeconds: 300, LimitSetBy: decide.LimitBySource}}, EvaluatedAt: epoch.Unix()})
	if err != nil {
		t.Fatal(err)
	}
	permit, err := reconcile.NewFreezeGate(nil).PermitTarget(false, "fin.cap.create_escalation_case", "fin.transaction:TX88213")
	if err != nil {
		t.Fatal(err)
	}
	sum := func(c string) contracts.CommonV1Hash {
		return contracts.CommonV1Hash("sha256:" + strings.Repeat(c, 64))
	}
	step := grants.Step{Permission: permit, TenantID: "ten_aa", CorrelationID: "corr_ABC123", DecisionID: "dec_01", PrincipalID: "usr_88213", IntentHash: sum("1"),
		CapabilityID: "fin.cap.create_escalation_case", TargetRef: "fin.transaction:TX88213", ParamsHash: sum("2"), EvidenceSetHash: sum("3"), PlanHash: sum("4"), PlanStep: 1,
		TargetVersions: []contracts.DecisionGrantV1TargetVersionsItem{{ResourceRef: "fin.transaction:TX88213", Source: "fin.src.ledger", VersionToken: "v7"}}}
	i, err := grants.NewIssuer(ring, ids.New(clock.NewManual(epoch), rand.New(rand.NewSource(1))), rand.New(rand.NewSource(2)), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	g, err := i.Issue(d, step)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestAKeyFileIsNamedByVersionAndHoldsOneLowerCaseHexSecret(t *testing.T) {
	good := map[string]string{"grant.hmac.1.key": hexOf("a"), "grant.hmac.12.key": hexOf("0") + "\n", "grant.hmac.999999.key": hexOf("f")}
	for name, content := range good {
		p := mustParse(t, name, content)
		if len(p.Key.Secret) != 32 || !strings.HasPrefix(p.Key.ID, keyfiles.IDPrefix) {
			t.Errorf("%s: %d bytes, id %q", name, len(p.Key.Secret), p.Key.ID)
		}
	}
	if p := mustParse(t, "grant.hmac.12.key", hexOf("0")); p.Version != 12 || p.Key.ID != "grant.hmac.12" {
		t.Errorf("version %d id %q", p.Version, p.Key.ID)
	}
	bad := map[string][2]string{
		"another name":       {"grant.key", hexOf("a")},
		"a version of zero":  {"grant.hmac.0.key", hexOf("a")},
		"a leading zero":     {"grant.hmac.01.key", hexOf("a")},
		"seven digits":       {"grant.hmac.1000000.key", hexOf("a")},
		"another identity":   {"orchestrator.hmac.1.key", hexOf("a")},
		"a path in the name": {"../grant.hmac.1.key", hexOf("a")},
		"upper-case hex":     {"grant.hmac.1.key", strings.Repeat("A", 64)},
		"63 characters":      {"grant.hmac.1.key", hexOf("a")[1:]},
		"65 characters":      {"grant.hmac.1.key", hexOf("a") + "a"},
		"not hex":            {"grant.hmac.1.key", strings.Repeat("g", 64)},
		"two lines":          {"grant.hmac.1.key", hexOf("a") + "\n" + hexOf("b")},
		"a carriage return":  {"grant.hmac.1.key", hexOf("a") + "\r\n"},
		"a leading space":    {"grant.hmac.1.key", " " + hexOf("a")},
		"empty":              {"grant.hmac.1.key", ""},
		"far too large":      {"grant.hmac.1.key", strings.Repeat("a", 4096)},
	}
	for label, c := range bad {
		if _, err := keyfiles.Parse(c[0], []byte(c[1])); err == nil {
			t.Errorf("%s: accepted", label)
		}
	}
}

func TestAnErrorNeverHoldsTheSecret(t *testing.T) {
	secret := strings.Repeat("c0de", 16) + "x" // 65 characters: refused, and its text must not be echoed
	_, err := keyfiles.Parse("grant.hmac.1.key", []byte(secret))
	if err == nil || strings.Contains(err.Error(), "c0de") {
		t.Fatalf("error %v", err)
	}
}

func TestTheHighestVersionSignsAndEitherVerifiesDuringARotation(t *testing.T) {
	old := mustParse(t, "grant.hmac.1.key", hexOf("1"))
	next := mustParse(t, "grant.hmac.2.key", hexOf("2"))
	// the order the files were read in does not matter
	for _, files := range [][]keyfiles.Parsed{{old, next}, {next, old}} {
		ring, err := keyfiles.Ring(files, keyfiles.Options{MaxActive: 2})
		if err != nil {
			t.Fatal(err)
		}
		if g := issue(t, ring); g.Sig.KeyId != "grant.hmac.2" {
			t.Fatalf("signed under %q, want the highest version", g.Sig.KeyId)
		}
	}
	oldOnly, _ := keyfiles.Ring([]keyfiles.Parsed{old}, keyfiles.Options{MaxActive: 2})
	both, _ := keyfiles.Ring([]keyfiles.Parsed{old, next}, keyfiles.Options{MaxActive: 2})
	newOnly, _ := keyfiles.Ring([]keyfiles.Parsed{next}, keyfiles.Options{MaxActive: 2})
	fromOld, fromNew := issue(t, oldOnly), issue(t, newOnly)
	if err := both.Verify(fromOld); err != nil {
		t.Errorf("the overlap must still verify a grant signed under the old key: %v", err)
	}
	if err := both.Verify(fromNew); err != nil {
		t.Errorf("the overlap must verify the new key: %v", err)
	}
	if err := newOnly.Verify(fromOld); err == nil {
		t.Error("a grant signed under a retired key verified")
	}
}

func TestTheRingRefusesWhatItCannotUseSafely(t *testing.T) {
	a := mustParse(t, "grant.hmac.1.key", hexOf("1"))
	b := mustParse(t, "grant.hmac.2.key", hexOf("2"))
	c := mustParse(t, "grant.hmac.3.key", hexOf("3"))
	same := mustParse(t, "grant.hmac.4.key", hexOf("1"))
	cases := map[string]struct {
		files []keyfiles.Parsed
		opt   keyfiles.Options
	}{
		"no key":                 {nil, keyfiles.Options{MaxActive: 2}},
		"no overlap decided":     {[]keyfiles.Parsed{a}, keyfiles.Options{}},
		"more keys than allowed": {[]keyfiles.Parsed{a, b}, keyfiles.Options{MaxActive: 1}},
		"three keys":             {[]keyfiles.Parsed{a, b, c}, keyfiles.Options{MaxActive: 3}},
		"a rotation that did not change the secret": {[]keyfiles.Parsed{a, same}, keyfiles.Options{MaxActive: 2}},
	}
	for label, tc := range cases {
		if _, err := keyfiles.Ring(tc.files, tc.opt); err == nil {
			t.Errorf("%s: accepted", label)
		}
	}
}

func TestAParsedKeyNeverPrintsItsSecret(t *testing.T) {
	p := mustParse(t, "grant.hmac.1.key", hexOf("c"))
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%x", "%d"} {
		out := fmt.Sprintf(verb, p) + fmt.Sprintf(verb, &p)
		if strings.Contains(out, "204") || strings.Contains(out, "cc") || !strings.Contains(out, "redacted") {
			t.Errorf("%s printed %q", verb, out)
		}
	}
}
