package grants_test

// SEC-09 (SR), REQ-SEC-005, INV-03, INV-25: the key-rotation drill for the Decision Grant HMAC key. Done when "rotation during a
// live run gives 0 rejected valid grants and 0 accepted forged grants". This is the drill on a simulated clock: a grant is issued every
// 30 seconds for 90 minutes while the key ring goes from one key, to two (the newest signs, either verifies), to the new key alone;
// at every tick the defender verifies every grant that is still inside its validity window, and a corpus of forgeries is presented.
// It is a test of the rule an operator must follow (retire the old key only after the last grant it signed has expired) and of the
// Keys type. It is not a test of a deployment: the Compose run, with the real secrets files, follows when TRU-05 is merged.
// All keys, IDs and values are SYNTHETIC.

import (
	"bytes"
	"math/rand"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/grants"
)

const (
	drillTick     = 30 * time.Second
	drillValidity = 10 * time.Minute
	drillLength   = 90 * time.Minute
	drillOverlap  = 30 * time.Minute // the second key joins here
)

var (
	keyOld      = bytes.Repeat([]byte{0x11}, 32)
	keyNew      = bytes.Repeat([]byte{0x22}, 32)
	keyAttacker = bytes.Repeat([]byte{0x33}, 32)
)

// drillGrant is one issued grant and when it lives.
type drillGrant struct {
	g       contracts.DecisionGrantV1
	issued  time.Duration // offset from the start of the run
	expires time.Duration
}

func ring(t *testing.T, ks ...grants.Key) *grants.Keys {
	t.Helper()
	k, err := grants.NewKeys(ks)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// allowAt is an ALLOW decision whose basis was evaluated at the given offset, which is what the grant's validity window starts from.
func allowAt(t *testing.T, at time.Duration) decide.Decision {
	t.Helper()
	r, _ := decide.Admit(decide.IntentView{Version: 2}).Resolved()
	d, err := r.Allow(decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass})
	if err != nil {
		t.Fatal(err)
	}
	b := basis()
	b.EvaluatedAt = epoch.Add(at).Unix()
	b.EvidenceFreshness = []decide.Freshness{{EvidenceID: "ev_1", EffectiveAt: b.EvaluatedAt - 30}}
	d, err = d.WithBasis(b)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func drillIssuer(t *testing.T, k *grants.Keys, seed int64) *grants.Issuer {
	t.Helper()
	i, err := grants.NewIssuer(k, ids.New(clock.NewManual(epoch), rand.New(rand.NewSource(seed))), rand.New(rand.NewSource(seed+1000)), drillValidity)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

// ringAt is the defender's key ring at an offset: one key, then two, then the new key alone from retireAt on.
func ringAt(t *testing.T, at, retireAt time.Duration) *grants.Keys {
	t.Helper()
	switch {
	case at < drillOverlap:
		return ring(t, grants.Key{ID: "grant.k1", Secret: keyOld})
	case at < retireAt:
		return ring(t, grants.Key{ID: "grant.k2", Secret: keyNew}, grants.Key{ID: "grant.k1", Secret: keyOld})
	default:
		return ring(t, grants.Key{ID: "grant.k2", Secret: keyNew})
	}
}

// run issues a grant per tick under the ring in force and returns them all. The issuer always signs with the newest key of its ring.
func run(t *testing.T, retireAt time.Duration) []drillGrant {
	t.Helper()
	var out []drillGrant
	for at := time.Duration(0); at < drillLength; at += drillTick {
		g, err := drillIssuer(t, ringAt(t, at, retireAt), int64(at/time.Second)+1).Issue(allowAt(t, at), step())
		if err != nil {
			t.Fatalf("issue at %v: %v", at, err)
		}
		out = append(out, drillGrant{g: g, issued: at, expires: at + drillValidity})
	}
	return out
}

// rejectedValid counts the checks, at every tick, where a grant still inside its validity window (and issued under a key that was in
// the ring when it was issued) fails verification against the ring in force at that tick.
func rejectedValid(t *testing.T, all []drillGrant, retireAt time.Duration) (rejected, checks int) {
	t.Helper()
	for at := time.Duration(0); at < drillLength+drillValidity; at += drillTick {
		defender := ringAt(t, at, retireAt)
		for _, dg := range all {
			if at < dg.issued || at >= dg.expires {
				continue
			}
			checks++
			if err := defender.Verify(dg.g); err != nil {
				rejected++
			}
		}
	}
	return rejected, checks
}

// forgeries builds the corpus an attacker without the current secrets can present at an offset. Every one must be refused.
func forgeries(t *testing.T, at time.Duration, genuine contracts.DecisionGrantV1) map[string]contracts.DecisionGrantV1 {
	t.Helper()
	out := map[string]contracts.DecisionGrantV1{}
	// 1. the right key IDs with a secret the attacker made up
	for _, id := range []string{"grant.k1", "grant.k2"} {
		g, err := drillIssuer(t, ring(t, grants.Key{ID: id, Secret: keyAttacker}), 77).Issue(allowAt(t, at), step())
		if err != nil {
			t.Fatal(err)
		}
		out["made-up secret under "+id] = g
	}
	// 2. a genuine grant with a field changed
	tampered := genuine
	tampered.ParamsHash = sha("9")
	out["tampered params_hash"] = tampered
	// 3. a genuine grant claiming the other key
	swapped := genuine
	if swapped.Sig.KeyId == "grant.k2" {
		swapped.Sig.KeyId = "grant.k1"
	} else {
		swapped.Sig.KeyId = "grant.k2"
	}
	out["key id swapped"] = swapped
	// 4. an unknown key and an empty signature
	unknown := genuine
	unknown.Sig.KeyId = "grant.k9"
	out["unknown key id"] = unknown
	empty := genuine
	empty.Sig.Value = ""
	out["empty signature"] = empty
	return out
}

// The drill with the safe rule: the old key is retired only after the last grant it signed has expired. 0 valid grants are rejected and
// 0 forgeries are accepted, at every tick of the run, and the run really exercised all three ring states.
func TestRotationDrillWithTheSafeRuleRejectsNoValidGrantAndAcceptsNoForgery(t *testing.T) {
	retireAt := drillOverlap + drillValidity // the last k1 grant was issued before drillOverlap, so it expires by this offset
	all := run(t, retireAt)
	signedBy := map[string]int{}
	for _, dg := range all {
		signedBy[dg.g.Sig.KeyId]++
	}
	if signedBy["grant.k1"] != int(drillOverlap/drillTick) || signedBy["grant.k2"] != int((drillLength-drillOverlap)/drillTick) {
		t.Fatalf("the run did not sign as planned: %v", signedBy)
	}
	rejected, checks := rejectedValid(t, all, retireAt)
	if checks < 1000 {
		t.Fatalf("the drill made only %d checks; it is not a drill", checks)
	}
	if rejected != 0 {
		t.Fatalf("%d of %d checks rejected a grant that was valid and unexpired", rejected, checks)
	}
	accepted, tried := 0, 0
	for at := time.Duration(0); at < drillLength; at += drillTick {
		genuine := all[int(at/drillTick)].g
		defender := ringAt(t, at, retireAt)
		for name, g := range forgeries(t, at, genuine) {
			tried++
			if defender.Verify(g) == nil {
				accepted++
				t.Errorf("at %v a forged grant was accepted: %s", at, name)
			}
		}
	}
	if accepted != 0 || tried < 500 {
		t.Fatalf("%d of %d forgeries accepted", accepted, tried)
	}
}

// The measurement can fail: retire the old key at the moment the new one joins, while k1 grants are still alive, and the drill counts
// rejected valid grants. Without this the zero above would prove nothing.
func TestRotationDrillCountsRejectedValidGrantsWhenTheOldKeyIsRetiredTooEarly(t *testing.T) {
	retireAt := drillOverlap // no overlap at all
	all := run(t, retireAt)
	rejected, _ := rejectedValid(t, all, retireAt)
	if rejected == 0 {
		t.Fatal("retiring the old key while its grants were still valid rejected nothing: the drill cannot detect the operator mistake")
	}
}

// What the overlap costs. While both keys are active a leaked OLD key forges a grant that verifies; from retirement on it does not.
// This is why the overlap is kept to the grant validity and why a suspected leak means retiring the old key at once, not waiting.
func TestALeakedOldKeyForgesDuringTheOverlapAndNotAfterRetirement(t *testing.T) {
	retireAt := drillOverlap + drillValidity
	leaked := func(at time.Duration) contracts.DecisionGrantV1 {
		g, err := drillIssuer(t, ring(t, grants.Key{ID: "grant.k1", Secret: keyOld}), 91).Issue(allowAt(t, at), step())
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	if err := ringAt(t, drillOverlap+time.Minute, retireAt).Verify(leaked(drillOverlap + time.Minute)); err != nil {
		t.Fatalf("expected the known window: during the overlap the old key still verifies, got %v", err)
	}
	if err := ringAt(t, retireAt, retireAt).Verify(leaked(retireAt)); err == nil {
		t.Fatal("a grant forged with the retired key was accepted after retirement")
	}
}
