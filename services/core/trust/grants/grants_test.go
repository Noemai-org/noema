package grants_test

// TRU-05 slice 1: a Decision Grant is built only from an ALLOW decision, one per write plan step, with every SoT 43.2 field, a nonce from a
// CSPRNG, and an HMAC-SHA-256 over the canonical JSON (JCS) with a key_id (REQ-TRU-008, INV-03, INV-25). Keys: the newest signs, either of
// two verifies during rotation, a retired key is rejected (REQ-SEC-005). All keys, IDs and values here are SYNTHETIC.

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/grants"
)

var (
	epoch = time.Date(2026, 11, 4, 16, 0, 0, 0, time.UTC)
	k1    = bytes.Repeat([]byte{1}, 32)
	k2    = bytes.Repeat([]byte{2}, 32)
	sha   = func(c string) contracts.CommonV1Hash {
		return contracts.CommonV1Hash("sha256:" + strings.Repeat(c, 64))
	}
)

func basis() decide.Basis {
	return decide.Basis{PackVersion: "fin.pack@1.0.0", PolicyRules: []decide.PolicyRuleVersion{{RuleID: "fin.pol.default.permit_case_writes", PolicyID: "fin.pol", Version: "3.1"}},
		EvidenceSnapshotID: "snap_01", EvidenceFreshness: []decide.Freshness{{EvidenceID: "ev_1", EffectiveAt: epoch.Unix() - 30}}, EvaluatedAt: epoch.Unix()}
}

func decision(t *testing.T, o decide.Outcome) decide.Decision {
	t.Helper()
	adm := decide.Admit(decide.IntentView{Version: 2})
	r, _ := adm.Resolved()
	pass := decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass}
	reason, err := decide.Core("NOT_AUTHORIZED")
	if err != nil {
		t.Fatal(err)
	}
	var d decide.Decision
	switch o {
	case decide.Allow:
		d, err = r.Allow(pass)
	case decide.Confirm:
		d, err = r.Confirm(pass)
	case decide.Reject:
		d, err = r.Reject(decide.Authority{Technical: decide.Pass, Policy: decide.Fail, Business: decide.NotEvaluated}, reason)
	case decide.Wait:
		d, err = r.Wait(pass, epoch.Unix()+60, reason)
	default:
		u, _ := decide.Admit(decide.IntentView{Version: 2, MaterialFlags: []string{"X"}}).Unresolved()
		if o == decide.Clarify {
			d, err = u.Clarify(decide.Clarification{}, reason)
		} else {
			d, err = u.Escalate(reason)
		}
	}
	if err != nil {
		t.Fatalf("%s: %v", o, err)
	}
	d, err = d.WithBasis(basis())
	if err != nil {
		t.Fatalf("%s basis: %v", o, err)
	}
	return d
}

func step() grants.Step {
	return grants.Step{
		TenantID: "tnt_a", CorrelationID: "corr_ABC123", DecisionID: "dec_01", PrincipalID: "usr_88213", IntentHash: sha("1"), CapabilityID: "fin.cap.create_escalation_case",
		TargetRef: "fin.transaction:TX88213", ParamsHash: sha("2"), EvidenceSetHash: sha("3"), PlanHash: sha("4"), PlanStep: 1,
		TargetVersions: []contracts.DecisionGrantV1TargetVersionsItem{{ResourceRef: "fin.transaction:TX88213", Source: "fin.src.ledger", VersionToken: "v7"}},
		PolicyVersion:  "fin.pol@3.1",
	}
}

func issuer(t *testing.T, keys *grants.Keys) *grants.Issuer {
	t.Helper()
	i, err := grants.NewIssuer(keys, ids.New(clock.NewManual(epoch), rand.New(rand.NewSource(1))), rand.New(rand.NewSource(2)), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func keys(t *testing.T) *grants.Keys {
	t.Helper()
	k, err := grants.NewKeys([]grants.Key{{ID: "k2", Secret: k2}, {ID: "k1", Secret: k1}})
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestAnAllowDecisionGivesAGrantThatBindsEveryField(t *testing.T) {
	k := keys(t)
	g, err := issuer(t, k).Issue(decision(t, decide.Allow), step())
	if err != nil {
		t.Fatal(err)
	}
	s := step()
	if g.TenantId != s.TenantID || g.DecisionId != s.DecisionID || g.PrincipalId != s.PrincipalID || g.IntentHash != s.IntentHash || g.CapabilityId != s.CapabilityID ||
		g.TargetRef != s.TargetRef || g.ParamsHash != s.ParamsHash || g.EvidenceSetHash != s.EvidenceSetHash || g.PlanHash != s.PlanHash || g.PlanStep != s.PlanStep ||
		len(g.TargetVersions) != 1 || g.TargetVersions[0] != s.TargetVersions[0] || g.PolicyVersion != "fin.pol@3.1" || g.PackVersion != "fin.pack@1.0.0" || !g.SingleUse {
		t.Fatalf("%+v", g)
	}
	if string(g.NotBefore) != "2026-11-04T16:00:00Z" || string(g.ExpiresAt) != "2026-11-04T16:10:00Z" {
		t.Fatalf("validity window: %s to %s", g.NotBefore, g.ExpiresAt)
	}
	if !strings.HasPrefix(string(g.GrantId), "grt_") || !strings.HasPrefix(g.Nonce, "n_") || len(g.Nonce) != 2+32 {
		t.Fatalf("ids: %s %s", g.GrantId, g.Nonce)
	}
	if g.Sig.KeyId != "k2" { // the newest key signs
		t.Fatalf("signed with %s", g.Sig.KeyId)
	}
	if err := k.Verify(g); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	var back contracts.DecisionGrantV1
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("the grant does not satisfy its contract: %v", err)
	}
}

// REQ-TRU-008: only an ALLOW issues a grant (grants where the decision is not ALLOW: 0).
func TestOnlyAnAllowDecisionIssuesAGrant(t *testing.T) {
	i := issuer(t, keys(t))
	for _, o := range []decide.Outcome{decide.Confirm, decide.Reject, decide.Wait, decide.Clarify, decide.Escalate} {
		if g, err := i.Issue(decision(t, o), step()); !errors.Is(err, grants.ErrNotAllow) || g.GrantId != "" {
			t.Errorf("%s: %v %+v", o, err, g)
		}
	}
	if _, err := i.Issue(decide.Decision{}, step()); !errors.Is(err, grants.ErrNotAllow) {
		t.Errorf("a zero decision: %v", err)
	}
}

func TestAStepThatDoesNotMatchTheDecisionIsRefused(t *testing.T) {
	i := issuer(t, keys(t))
	d := decision(t, decide.Allow)
	for name, mod := range map[string]func(*grants.Step){
		"a policy version the decision did not use": func(s *grants.Step) { s.PolicyVersion = "fin.pol@9.9" },
		"no policy version":                         func(s *grants.Step) { s.PolicyVersion = "" },
		"no tenant":                                 func(s *grants.Step) { s.TenantID = "" },
		"no principal":                              func(s *grants.Step) { s.PrincipalID = "" },
		"no target versions":                        func(s *grants.Step) { s.TargetVersions = nil },
		"a negative plan step":                      func(s *grants.Step) { s.PlanStep = -1 },
		"no params hash":                            func(s *grants.Step) { s.ParamsHash = "" },
		"no evidence set hash":                      func(s *grants.Step) { s.EvidenceSetHash = "" },
		"no plan hash":                              func(s *grants.Step) { s.PlanHash = "" },
		"no capability":                             func(s *grants.Step) { s.CapabilityID = "" },
		"no target":                                 func(s *grants.Step) { s.TargetRef = "" },
	} {
		s := step()
		mod(&s)
		if g, err := i.Issue(d, s); !errors.Is(err, grants.ErrStep) || g.GrantId != "" {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestEveryGrantHasItsOwnIDAndNonceAndAnEntropyFailureIsAnError(t *testing.T) {
	i := issuer(t, keys(t))
	d := decision(t, decide.Allow)
	seenID, seenNonce := map[contracts.CommonV1GrantId]bool{}, map[string]bool{}
	for n := 0; n < 500; n++ {
		g, err := i.Issue(d, step())
		if err != nil {
			t.Fatal(err)
		}
		if seenID[g.GrantId] || seenNonce[g.Nonce] {
			t.Fatalf("a repeated id or nonce at %d", n)
		}
		seenID[g.GrantId], seenNonce[g.Nonce] = true, true
	}
	empty := bytes.NewReader(nil)
	broken, err := grants.NewIssuer(keys(t), ids.New(clock.NewManual(epoch), rand.New(rand.NewSource(1))), empty, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if g, err := broken.Issue(d, step()); err == nil || g.GrantId != "" {
		t.Fatalf("an entropy failure must be an error, not a grant: %v", err)
	}
}

// The signature binds every field: changing any one makes verification fail, 100% of the time (EC-X09).
func TestAnyChangeToAGrantBreaksItsSignature(t *testing.T) {
	k := keys(t)
	g, err := issuer(t, k).Issue(decision(t, decide.Allow), step())
	if err != nil {
		t.Fatal(err)
	}
	mods := map[string]func(*contracts.DecisionGrantV1){
		"tenant":       func(x *contracts.DecisionGrantV1) { x.TenantId = "tnt_b" },
		"correlation":  func(x *contracts.DecisionGrantV1) { x.CorrelationId = "corr_OTHER" },
		"grant id":     func(x *contracts.DecisionGrantV1) { x.GrantId = "grt_OTHER" },
		"decision":     func(x *contracts.DecisionGrantV1) { x.DecisionId = "dec_02" },
		"principal":    func(x *contracts.DecisionGrantV1) { x.PrincipalId = "usr_99999" },
		"intent":       func(x *contracts.DecisionGrantV1) { x.IntentHash = sha("9") },
		"capability":   func(x *contracts.DecisionGrantV1) { x.CapabilityId = "fin.cap.log_complaint_reference" },
		"target":       func(x *contracts.DecisionGrantV1) { x.TargetRef = "fin.transaction:TX00001" },
		"params":       func(x *contracts.DecisionGrantV1) { x.ParamsHash = sha("9") },
		"evidence":     func(x *contracts.DecisionGrantV1) { x.EvidenceSetHash = sha("9") },
		"plan":         func(x *contracts.DecisionGrantV1) { x.PlanHash = sha("9") },
		"plan step":    func(x *contracts.DecisionGrantV1) { x.PlanStep++ },
		"version":      func(x *contracts.DecisionGrantV1) { x.TargetVersions[0].VersionToken = "v8" },
		"extra target": func(x *contracts.DecisionGrantV1) { x.TargetVersions = append(x.TargetVersions, x.TargetVersions[0]) },
		"policy":       func(x *contracts.DecisionGrantV1) { x.PolicyVersion = "fin.pol@3.2" },
		"pack":         func(x *contracts.DecisionGrantV1) { x.PackVersion = "fin.pack@1.0.1" },
		"not before":   func(x *contracts.DecisionGrantV1) { x.NotBefore = "2026-11-04T15:00:00Z" },
		"expiry":       func(x *contracts.DecisionGrantV1) { x.ExpiresAt = "2026-11-04T17:00:00Z" },
		"nonce":        func(x *contracts.DecisionGrantV1) { x.Nonce = "n_" + strings.Repeat("0", 32) },
		"single use":   func(x *contracts.DecisionGrantV1) { x.SingleUse = false },
		"confirmation": func(x *contracts.DecisionGrantV1) {
			c := contracts.CommonV1ConfirmationEventId("cfe_01")
			x.ConfirmationEventId = &c
		},
		"key id":       func(x *contracts.DecisionGrantV1) { x.Sig.KeyId = "k1" },
		"signature":    func(x *contracts.DecisionGrantV1) { x.Sig.Value = strings.Repeat("0", 64) },
		"no signature": func(x *contracts.DecisionGrantV1) { x.Sig.Value = "" },
	}
	for name, mod := range mods {
		c := g
		c.TargetVersions = append([]contracts.DecisionGrantV1TargetVersionsItem(nil), g.TargetVersions...)
		mod(&c)
		if err := k.Verify(c); !errors.Is(err, grants.ErrSignature) {
			t.Errorf("%s: got %v", name, err)
		}
	}
	if err := k.Verify(g); err != nil {
		t.Fatalf("the unmodified grant must verify: %v", err)
	}
}

// REQ-SEC-005: two keys are active during a rotation: the newest signs, either verifies; after retirement the old key is rejected.
func TestRotationAcceptsBothKeysThenRejectsTheRetiredOne(t *testing.T) {
	old, err := grants.NewKeys([]grants.Key{{ID: "k1", Secret: k1}})
	if err != nil {
		t.Fatal(err)
	}
	gOld, err := issuer(t, old).Issue(decision(t, decide.Allow), step())
	if err != nil || gOld.Sig.KeyId != "k1" {
		t.Fatalf("%v %s", err, gOld.Sig.KeyId)
	}
	overlap := keys(t) // k2 newest, k1 still active
	gNew, err := issuer(t, overlap).Issue(decision(t, decide.Allow), step())
	if err != nil || gNew.Sig.KeyId != "k2" {
		t.Fatalf("%v %s", err, gNew.Sig.KeyId)
	}
	if overlap.Verify(gOld) != nil || overlap.Verify(gNew) != nil {
		t.Fatal("during the overlap both keys must verify")
	}
	retired, err := grants.NewKeys([]grants.Key{{ID: "k2", Secret: k2}})
	if err != nil {
		t.Fatal(err)
	}
	if err := retired.Verify(gOld); !errors.Is(err, grants.ErrSignature) {
		t.Fatalf("a grant under a retired key must be rejected: %v", err)
	}
	if err := retired.Verify(gNew); err != nil {
		t.Fatal(err)
	}
	// a grant that claims one key but was signed with another is rejected
	forged := gNew
	forged.Sig.KeyId = "k1"
	if err := overlap.Verify(forged); !errors.Is(err, grants.ErrSignature) {
		t.Fatalf("%v", err)
	}
}

func TestKeysRefuseWhatCannotBeUsedSafely(t *testing.T) {
	for name, ks := range map[string][]grants.Key{
		"none":            nil,
		"three":           {{ID: "a", Secret: k1}, {ID: "b", Secret: k2}, {ID: "c", Secret: bytes.Repeat([]byte{3}, 32)}},
		"a short secret":  {{ID: "a", Secret: []byte("short")}},
		"an empty ID":     {{ID: "", Secret: k1}},
		"a repeated ID":   {{ID: "a", Secret: k1}, {ID: "a", Secret: k2}},
		"the same secret": {{ID: "a", Secret: k1}, {ID: "b", Secret: k1}},
	} {
		if _, err := grants.NewKeys(ks); !errors.Is(err, grants.ErrKeys) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := grants.NewIssuer(nil, nil, nil, time.Minute); err == nil {
		t.Error("an issuer without keys, ids or entropy must not start")
	}
	if _, err := grants.NewIssuer(keys(t), ids.New(clock.NewManual(epoch), nil), bytes.NewReader(make([]byte, 64)), 0); err == nil {
		t.Error("an issuer with no validity must not start")
	}
}

// The canonical form is what is signed: field order and whitespace in the JSON the grant travels in do not matter (REQ-TRU-008: JCS).
func TestTheSignatureIsOverTheCanonicalJSON(t *testing.T) {
	k := keys(t)
	g, _ := issuer(t, k).Issue(decision(t, decide.Allow), step())
	want, err := crypto.CanonicalJSON(withoutSig(t, g))
	if err != nil {
		t.Fatal(err)
	}
	if !k.VerifyBytes(g.Sig, want) {
		t.Fatal("the signature is not over the JCS form of the grant without sig")
	}
	pretty, _ := json.MarshalIndent(g, "", "    ")
	var back contracts.DecisionGrantV1
	if err := json.Unmarshal(pretty, &back); err != nil {
		t.Fatal(err)
	}
	if err := k.Verify(back); err != nil {
		t.Fatalf("a re-spelled but equal grant must verify: %v", err)
	}
}

func withoutSig(t *testing.T, g contracts.DecisionGrantV1) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(g)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	delete(m, "sig")
	return m
}
