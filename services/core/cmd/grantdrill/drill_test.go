package main

// SEC-09 (SR), REQ-SEC-005, INV-03, INV-25: the drill's own logic, with rings made from parsed key files (no file is opened, so these run on every system). The drill must be able to fail:
// each test below makes the run go wrong in one way and requires the count that says so. All keys are SYNTHETIC.

import (
	"bytes"
	"encoding/hex"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/grants/keyfiles"
)

var (
	secretOld      = bytes.Repeat([]byte{0x11}, 32)
	secretNew      = bytes.Repeat([]byte{0x22}, 32)
	secretAttacker = bytes.Repeat([]byte{0x33}, 32)
)

func parsed(t *testing.T, version string, secret []byte) keyfiles.Parsed {
	t.Helper()
	p, err := keyfiles.Parse("grant.hmac."+version+".key", []byte(hex.EncodeToString(secret)+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func ringOf(t *testing.T, files ...keyfiles.Parsed) *grants.Keys {
	t.Helper()
	k, err := keyfiles.Ring(files, keyfiles.Options{MaxActive: 2})
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// operator is the order an operator follows; keepOld leaves the old key in the ring after the time it should have been retired; leak puts the attacker's secret in under the old key's id.
func operator(t *testing.T, retireAt time.Duration, keepOld, leak bool) ringAt {
	t.Helper()
	oldKey := parsed(t, "1", secretOld)
	if leak {
		oldKey = parsed(t, "1", secretAttacker)
	}
	one, both, only := ringOf(t, oldKey), ringOf(t, parsed(t, "2", secretNew), oldKey), ringOf(t, parsed(t, "2", secretNew))
	return func(at time.Duration) (*grants.Keys, error) {
		switch {
		case at < overlap:
			return one, nil
		case at < retireAt || keepOld:
			return both, nil
		default:
			return only, nil
		}
	}
}

func drill(t *testing.T, retireAt time.Duration, keepOld, leak bool) result {
	t.Helper()
	r, err := run(plan{ring: operator(t, retireAt, keepOld, leak), retireAt: retireAt, oldSecret: secretOld, attacker: secretAttacker})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestTheSafeRuleRejectsNoValidGrantAndAcceptsNoForgery(t *testing.T) {
	r := drill(t, overlap+validity, false, false)
	if f := r.failures(); len(f) != 0 {
		t.Fatalf("%v (%+v)", f, r)
	}
	if r.issued != int(length/tick) || r.signedOld != int(overlap/tick) || r.signedNew != int((length-overlap)/tick) {
		t.Fatalf("the run did not sign as planned: %+v", r)
	}
}

func TestRetiringTheOldKeyTooEarlyIsCountedAsRejectedValidGrants(t *testing.T) {
	r := drill(t, overlap, false, false)
	if r.rejectedValid == 0 {
		t.Fatalf("no valid grant was rejected when the old key was retired while its grants were live: %+v", r)
	}
	if len(r.failures()) == 0 {
		t.Fatal("failures() did not report it")
	}
}

func TestAnOldKeyThatIsNeverRetiredIsCountedAsAcceptedForgery(t *testing.T) {
	r := drill(t, overlap+validity, true, false)
	if r.forgedAccepted == 0 {
		t.Fatalf("a grant signed with the retired key was accepted and not counted: %+v", r)
	}
	if len(r.failures()) == 0 {
		t.Fatal("failures() did not report it")
	}
}

func TestARingThatHoldsTheAttackersSecretIsCountedAsAcceptedForgery(t *testing.T) {
	r := drill(t, overlap+validity, false, true)
	if r.forgedAccepted == 0 {
		t.Fatalf("forgeries made with the attacker's secret were accepted and not counted: %+v", r)
	}
}

func TestARunThatCannotBuildItsRingStopsInsteadOfPassing(t *testing.T) {
	_, err := run(plan{ring: func(time.Duration) (*grants.Keys, error) { return nil, keyfiles.ErrKeyFiles }, retireAt: overlap + validity, oldSecret: secretOld, attacker: secretAttacker})
	if err == nil {
		t.Fatal("a ring that could not be loaded produced a result")
	}
}

func TestAResultWithTooFewChecksOrOnlyOneKeyIsNotAPass(t *testing.T) {
	if len((result{}).failures()) == 0 {
		t.Error("an empty result passed")
	}
	r := drill(t, overlap+validity, false, false)
	r.signedNew = 0
	if len(r.failures()) == 0 {
		t.Error("a run that never signed under the new key passed")
	}
}
