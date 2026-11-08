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
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
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

func decisionWithRules(t *testing.T, rules ...decide.PolicyRuleVersion) decide.Decision {
	t.Helper()
	r, _ := decide.Admit(decide.IntentView{Version: 2}).Resolved()
	d, err := r.Allow(decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass})
	if err != nil {
		t.Fatal(err)
	}
	b := basis()
	b.PolicyRules = rules
	d, err = d.WithBasis(b)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// permit is the freeze Permission for a capability on a target that is not frozen: the only way to get one is the gate.
func permit(t testing.TB, capability string) reconcile.Permission {
	t.Helper()
	return mustPermit(capability)
}

func step() grants.Step {
	return grants.Step{Permission: mustPermit("fin.cap.create_escalation_case"),
		TenantID: "ten_aa", CorrelationID: "corr_ABC123", DecisionID: "dec_01", PrincipalID: "usr_88213", IntentHash: sha("1"), CapabilityID: "fin.cap.create_escalation_case",
		TargetRef: "fin.transaction:TX88213", ParamsHash: sha("2"), EvidenceSetHash: sha("3"), PlanHash: sha("4"), PlanStep: 1,
		TargetVersions: []contracts.DecisionGrantV1TargetVersionsItem{{ResourceRef: "fin.transaction:TX88213", Source: "fin.src.ledger", VersionToken: "v7"}},
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
	if string(g.TenantId) != s.TenantID || string(g.DecisionId) != s.DecisionID || string(g.PrincipalId) != s.PrincipalID || g.IntentHash != s.IntentHash ||
		string(g.CapabilityId) != s.CapabilityID || string(g.TargetRef) != s.TargetRef || g.ParamsHash != s.ParamsHash || g.EvidenceSetHash != s.EvidenceSetHash || g.PlanHash != s.PlanHash || g.PlanStep != s.PlanStep ||
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
	// an ALLOW that was never given its basis rests on nothing and issues nothing
	r, _ := decide.Admit(decide.IntentView{Version: 2}).Resolved()
	bare, err := r.Allow(decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := i.Issue(bare, step()); !errors.Is(err, grants.ErrNotAllow) {
		t.Errorf("an ALLOW without a basis: %v", err)
	}
	minimal, err := bare.WithBasis(decide.Basis{PackVersion: "fin.pack@1.0.0", EvaluatedAt: epoch.Unix()})
	if err == nil {
		if _, err := i.Issue(minimal, step()); !errors.Is(err, grants.ErrNotAllow) {
			t.Errorf("an ALLOW with a minimal basis: %v", err)
		}
	}
}

func TestAStepThatDoesNotMatchTheDecisionIsRefused(t *testing.T) {
	i := issuer(t, keys(t))
	d := decision(t, decide.Allow)
	for name, mod := range map[string]func(*grants.Step){
		"no tenant":            func(s *grants.Step) { s.TenantID = "" },
		"no principal":         func(s *grants.Step) { s.PrincipalID = "" },
		"a negative plan step": func(s *grants.Step) { s.PlanStep = -1 },
		"no params hash":       func(s *grants.Step) { s.ParamsHash = "" },
		"no evidence set hash": func(s *grants.Step) { s.EvidenceSetHash = "" },
		"no plan hash":         func(s *grants.Step) { s.PlanHash = "" },
		"no capability":        func(s *grants.Step) { s.CapabilityID = "" },
		"no target":            func(s *grants.Step) { s.TargetRef = "" },
	} {
		s := step()
		mod(&s)
		if g, err := i.Issue(d, s); !errors.Is(err, grants.ErrStep) || g.GrantId != "" {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Ruling committed by the Repo Steward (Architect, 5 Nov 2026): a grant binds the exact policy_id and version it was decided under.
// Versions compare only within the same policy_id (as dotted numbers: 3.10 is newer than 3.9); the newest across policy IDs is never picked.
// A basis that names more than one policy ID needs the step to say which one the grant is for; a policy ID the basis does not name is refused.
func TestAGrantBindsTheExactPolicyItWasDecidedUnder(t *testing.T) {
	i := issuer(t, keys(t))
	rule := func(id, pol, ver string) decide.PolicyRuleVersion {
		return decide.PolicyRuleVersion{RuleID: id, PolicyID: pol, Version: ver}
	}
	for name, c := range map[string]struct {
		rules  []decide.PolicyRuleVersion
		policy string // Step.PolicyID; empty when the basis names one policy
		want   string
	}{
		"one version": {[]decide.PolicyRuleVersion{rule("a", "fin.pol", "3.1")}, "", "fin.pol@3.1"},
		"two versions of one policy, newest last":  {[]decide.PolicyRuleVersion{rule("a", "fin.pol", "3.1"), rule("b", "fin.pol", "3.2")}, "", "fin.pol@3.2"},
		"two versions of one policy, newest first": {[]decide.PolicyRuleVersion{rule("b", "fin.pol", "3.2"), rule("a", "fin.pol", "3.1")}, "", "fin.pol@3.2"},
		"numeric, not text":                        {[]decide.PolicyRuleVersion{rule("a", "fin.pol", "3.9"), rule("b", "fin.pol", "3.10")}, "", "fin.pol@3.10"},
		"three parts":                              {[]decide.PolicyRuleVersion{rule("a", "fin.pol", "3.1"), rule("b", "fin.pol", "3.1.2")}, "", "fin.pol@3.1.2"},
		"repeated version":                         {[]decide.PolicyRuleVersion{rule("a", "fin.pol", "3.1"), rule("b", "fin.pol", "3.1")}, "", "fin.pol@3.1"},
		"two policies: the named one, though the other has a higher version":      {[]decide.PolicyRuleVersion{rule("a", "fin.aaa", "9.0"), rule("b", "fin.zzz", "1.0")}, "fin.zzz", "fin.zzz@1.0"},
		"two policies: the named one, in the other order":                         {[]decide.PolicyRuleVersion{rule("b", "fin.zzz", "1.0"), rule("a", "fin.aaa", "9.0")}, "fin.zzz", "fin.zzz@1.0"},
		"the same version string under two policy IDs: naming one gives that one": {[]decide.PolicyRuleVersion{rule("a", "fin.aaa", "1.0"), rule("b", "fin.zzz", "1.0")}, "fin.zzz", "fin.zzz@1.0"},
		"the same version string under two policy IDs: naming the other gives it": {[]decide.PolicyRuleVersion{rule("a", "fin.aaa", "1.0"), rule("b", "fin.zzz", "1.0")}, "fin.aaa", "fin.aaa@1.0"},
		"a leading-zero version is schema-valid (Codex P2)":                       {[]decide.PolicyRuleVersion{rule("a", "fin.pol", "03.01")}, "", "fin.pol@03.01"},
		"leading zeros compare as numbers: 3.010 is newer than 3.9":               {[]decide.PolicyRuleVersion{rule("a", "fin.pol", "3.9"), rule("b", "fin.pol", "3.010")}, "", "fin.pol@3.010"},
		"two policies: the named one with two versions":                           {[]decide.PolicyRuleVersion{rule("a", "fin.aaa", "9.0"), rule("b", "fin.zzz", "1.0"), rule("c", "fin.zzz", "1.1")}, "fin.zzz", "fin.zzz@1.1"},
	} {
		s := step()
		s.PolicyID = c.policy
		g, err := i.Issue(decisionWithRules(t, c.rules...), s)
		if err != nil || g.PolicyVersion != c.want {
			t.Errorf("%s: got %q, %v, want %q", name, g.PolicyVersion, err, c.want)
		}
	}
	// refused, never guessed: two policies and none named; a policy the basis does not name; a version the contract cannot carry
	two := []decide.PolicyRuleVersion{rule("a", "fin.aaa", "9.0"), rule("b", "fin.zzz", "1.0")}
	for name, c := range map[string]struct {
		rules  []decide.PolicyRuleVersion
		policy string
	}{
		"two policies and none named":          {two, ""},
		"a policy the basis does not name":     {two, "fin.mmm"},
		"a policy named but none in the basis": {[]decide.PolicyRuleVersion{rule("a", "fin.pol", "3.1")}, "fin.other"},
	} {
		s := step()
		s.PolicyID = c.policy
		if g, err := i.Issue(decisionWithRules(t, c.rules...), s); !errors.Is(err, grants.ErrStep) || g.GrantId != "" {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, bad := range []string{"three", "3..1", "3.", "-1.0", "3.1-rc1", "+3.1", "3.1 ", "\u0663.\u0661"} {
		if g, err := i.Issue(decisionWithRules(t, rule("a", "fin.pol", bad)), step()); !errors.Is(err, grants.ErrStep) || g.GrantId != "" {
			t.Errorf("a malformed version %q: %v", bad, err)
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
		"tenant":       func(x *contracts.DecisionGrantV1) { x.TenantId = "ten_bb" },
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
	// Codex P2 on noema#163: the timestamps have whole seconds, so a window under one second would be written with expires_at equal to not_before
	// and E2 would treat the grant as expired on arrival.
	for _, d := range []time.Duration{time.Millisecond, 999 * time.Millisecond} {
		if _, err := grants.NewIssuer(keys(t), ids.New(clock.NewManual(epoch), nil), bytes.NewReader(make([]byte, 64)), d); err == nil {
			t.Errorf("an issuer with a validity of %s must not start", d)
		}
	}
	if _, err := grants.NewIssuer(keys(t), ids.New(clock.NewManual(epoch), nil), bytes.NewReader(make([]byte, 64)), time.Second); err != nil {
		t.Errorf("a validity of one second: %v", err)
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
	if !k.VerifyBytes(g.Sig, crypto.GrantSigningMessage(want)) {
		t.Fatal("the signature is not over the grant label, a 0x00 byte and the JCS form of the grant without sig")
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
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatal(err)
	}
	delete(m, "sig")
	return m
}

// Tamper fuzzing (TRU-05 done-when): random byte changes to a grant's JSON are rejected 100% of the time: the text no longer parses as a
// grant, or the signature does not verify, or the grant is unchanged. Seeded.
func TestTamperFuzzingIsRejected(t *testing.T) {
	k := keys(t)
	g, err := issuer(t, k).Issue(decision(t, decide.Allow), step())
	if err != nil {
		t.Fatal(err)
	}
	orig, _ := json.Marshal(g)
	rng := rand.New(rand.NewSource(20261104))
	parsed, accepted := 0, 0
	for n := 0; n < 20_000; n++ {
		b := append([]byte(nil), orig...)
		for flips := 1 + rng.Intn(3); flips > 0; flips-- {
			b[rng.Intn(len(b))] = byte(32 + rng.Intn(95))
		}
		var back contracts.DecisionGrantV1
		if json.Unmarshal(b, &back) != nil {
			continue
		}
		parsed++
		again, _ := json.Marshal(back)
		if bytes.Equal(again, orig) {
			continue // the change did not alter the grant (for example inside whitespace or an equal respelling)
		}
		if k.Verify(back) == nil {
			accepted++
		}
	}
	if accepted != 0 {
		t.Fatalf("%d of %d parsed mutations verified", accepted, parsed)
	}
	if parsed < 2000 {
		t.Fatalf("only %d mutations parsed: the fuzz is too narrow to prove anything", parsed)
	}
}

// Codex P1 on noema#163 (REQ-RES-002): a grant cannot be built for a frozen target and a capability that is not allowed under a freeze. The
// proof is a Permission, whose only constructor is FreezeGate.Permit; the issuer must require one bound to the step's own capability.
func TestAGrantNeedsAFreezePermissionForItsOwnCapability(t *testing.T) {
	gate := reconcile.NewFreezeGate([]string{"fin.cap.attach_evidence"})
	i := issuer(t, keys(t))
	d := decision(t, decide.Allow)
	own, err := gate.PermitTarget(false, "fin.cap.create_escalation_case", "fin.transaction:TX88213")
	if err != nil {
		t.Fatal(err)
	}
	other, err := gate.PermitTarget(false, "fin.cap.attach_evidence", "fin.transaction:TX88213")
	if err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]reconcile.Permission{"no permission (the zero value)": {}, "a permission for another capability": other} {
		st := step()
		st.Permission = p
		if g, err := i.Issue(d, st); !errors.Is(err, grants.ErrNoPermission) {
			t.Errorf("%s: err %v, grant %+v; want ErrNoPermission and no grant", name, err, g)
		}
	}
	st := step()
	st.Permission = own
	if _, err := i.Issue(d, st); err != nil {
		t.Fatalf("a permission for the step's own capability: %v", err)
	}
	// and on a frozen target the gate itself refuses the capability that is not allowed, so there is no permission to present
	if _, err := gate.Permit(true, "fin.cap.create_escalation_case"); err == nil {
		t.Fatal("the gate gave a permission for a frozen target and a capability that is not allowed under a freeze")
	}
}

// Codex P2 on noema#163: the pack version is copied into the signed grant, so it must match the grant contract's pattern, like the policy
// version; a basis only guarantees that it is not empty.
func TestAGrantIsNotSignedForAMalformedPackVersion(t *testing.T) {
	i := issuer(t, keys(t))
	for _, bad := range []string{"fintech", "fin.pack@1", "Fin.Pack@1.0", "fin.pack@1.x", "fin.pack@1.0.0.0", "fin.pack@", "@1.0", "fin pack@1.0", "fin.pack@1.0\n"} {
		r, _ := decide.Admit(decide.IntentView{Version: 2}).Resolved()
		d, err := r.Allow(decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass})
		if err != nil {
			t.Fatal(err)
		}
		b := basis()
		b.PackVersion = bad
		if d, err = d.WithBasis(b); err != nil {
			t.Fatal(err)
		}
		st := step()
		st.Permission = permit(t, st.CapabilityID)
		if g, err := i.Issue(d, st); !errors.Is(err, grants.ErrStep) {
			t.Errorf("pack version %q: err %v, grant %+v; want ErrStep and no grant", bad, err, g)
		}
	}
}

func mustPermit(capability string) reconcile.Permission {
	p, err := reconcile.NewFreezeGate(nil).PermitTarget(false, capability, "fin.transaction:TX88213")
	if err != nil {
		panic(err)
	}
	return p
}

// Codex P1 on noema#163 at 4546400: a permission obtained for one target must not open a grant for another. The proof names the target as well as
// the capability; a permission that names no target (the older Permit) opens nothing.
func TestAFreezePermissionIsBoundToTheTargetOfTheGrant(t *testing.T) {
	gate := reconcile.NewFreezeGate(nil)
	i := issuer(t, keys(t))
	d := decision(t, decide.Allow)
	const capability, target = "fin.cap.create_escalation_case", "fin.transaction:TX88213"
	other, err := gate.PermitTarget(false, capability, "fin.transaction:TX00001")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := gate.Permit(false, capability)
	if err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]reconcile.Permission{"a permission for another target": other, "a permission that names no target": legacy} {
		st := step()
		st.Permission = p
		if g, err := i.Issue(d, st); !errors.Is(err, grants.ErrNoPermission) {
			t.Errorf("%s: err %v, grant %+v; want ErrNoPermission and no grant", name, err, g)
		}
	}
	own, err := gate.PermitTarget(false, capability, target)
	if err != nil {
		t.Fatal(err)
	}
	st := step()
	st.Permission = own
	if _, err := i.Issue(d, st); err != nil {
		t.Fatalf("a permission for the step's own capability and target: %v", err)
	}
	// the gate refuses a frozen target for a capability that is not allowed under a freeze, and a permission with no target
	if _, err := gate.PermitTarget(true, capability, target); err == nil {
		t.Fatal("the gate gave a permission for a frozen target and a capability that is not allowed under a freeze")
	}
	if _, err := gate.PermitTarget(false, capability, ""); err == nil {
		t.Fatal("the gate gave a permission that names no target")
	}
}

// SR on noema#163 (finding 2): a loader that zeroes its buffer after constructing the keys must not turn the signing key into zeros. NewKeys hands the
// secret to the key ring, which copies it; this pins that, so a later change that keeps the caller's slice fails here.
func TestKeysDoNotAliasTheCallersSecret(t *testing.T) {
	secret := append([]byte(nil), k1...)
	ks, err := grants.NewKeys([]grants.Key{{ID: "k1", Secret: secret}})
	if err != nil {
		t.Fatal(err)
	}
	before, err := issuer(t, ks).Issue(decision(t, decide.Allow), step())
	if err != nil {
		t.Fatal(err)
	}
	for i := range secret {
		secret[i] = 0 // the loader wipes its buffer
	}
	if err := ks.Verify(before); err != nil {
		t.Fatalf("a grant signed before the caller's buffer was wiped no longer verifies: %v", err)
	}
	after, err := issuer(t, ks).Issue(decision(t, decide.Allow), step())
	if err != nil {
		t.Fatal(err)
	}
	if after.Sig.Value == "" || ks.Verify(after) != nil {
		t.Fatal("a grant signed after the buffer was wiped must still be signed with the real key")
	}
	other, err := grants.NewKeys([]grants.Key{{ID: "k1", Secret: k1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Verify(after); err != nil {
		t.Fatalf("the key in use after the wipe is not the original secret: %v", err)
	}
}

// Ruling of 8 Nov 2026 on noema#292 (SR on noema#163, finding 1): a grant signature covers the domain label, a 0x00 byte and the JCS bytes. A signature over
// bare JCS, or under the confirmation-request label, is not a grant signature, so a signer for another object type under the same key cannot mint a grant.
func TestAGrantSignedOverBareJCSOrUnderAnotherLabelIsRefused(t *testing.T) {
	ks := keys(t)
	g, err := issuer(t, ks).Issue(decision(t, decide.Allow), step())
	if err != nil {
		t.Fatal(err)
	}
	if err := ks.Verify(g); err != nil {
		t.Fatalf("the issued grant must verify: %v", err)
	}
	raw, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatal(err)
	}
	delete(m, "sig")
	canon, err := crypto.CanonicalJSON(m)
	if err != nil {
		t.Fatal(err)
	}
	ring := crypto.NewKeyRing(map[string][]byte{g.Sig.KeyId: k2})
	for name, msg := range map[string][]byte{
		"bare JCS":                         canon,
		"the confirmation-request label":   crypto.ConfirmationRequestSigningMessage(canon),
		"the grant label without the 0x00": append([]byte(crypto.GrantLabel), canon...),
	} {
		sig, err := ring.SignWith(g.Sig.KeyId, msg)
		if err != nil {
			t.Fatal(err)
		}
		forged := g
		forged.Sig.Value = sig
		if err := ks.Verify(forged); !errors.Is(err, grants.ErrSignature) {
			t.Errorf("a grant signed over %s must be refused, got %v", name, err)
		}
	}
}
