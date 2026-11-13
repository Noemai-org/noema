package chain_test

import (
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/recordsig"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// A184, A154: the scan of every test Record. testpg runs it on every test database when the test ends, so a fixture that reaches the Record by raw SQL cannot put a long
// digit run into it, and no code path can write an object_ref made from a payload string. These tests pin the scan itself: it is clean on a good Record, it finds a stored
// digit run and a derived object_ref, and it names the event and the path, never the value.

func scan(t *testing.T, w *world) []string {
	t.Helper()
	found, err := testpg.ScanRecord(ctx(t), w.dsn)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	return found
}

func TestTheScanOfTheRecordIsCleanOnAGoodRecord(t *testing.T) {
	w := newWorld(t, "ten_aa")
	w.append(t, payloadEvent("ten_aa", 1, map[string]any{"note": "PIN 1234", "at": "2026-11-13T10:12:00Z", "amount": 2000000000}))
	w.append(t, event("ten_aa", 2))
	if found := scan(t, w); len(found) != 0 {
		t.Fatalf("the scan found %v in a good Record", found)
	}
}

func TestTheScanOfTheRecordFindsAStoredDigitRunAndNamesNeverTheValue(t *testing.T) {
	w := newWorld(t, "ten_aa")
	testpg.AllowUnguardedRecord(t, w.dsn) // this test stores the run on purpose, to prove that the scan finds it
	st := appendUnguarded(t, w, payloadEvent("ten_aa", 1, map[string]any{"outer": map[string]any{"note": "card 4111111111111111"}}))
	found := scan(t, w)
	if len(found) != 1 {
		t.Fatalf("the scan found %v, want one finding", found)
	}
	if !strings.Contains(found[0], st.EventID) || !strings.Contains(found[0], "/outer/note") {
		t.Fatalf("the finding does not name the event and the path: %q", found[0])
	}
	if strings.Contains(found[0], "4111") {
		t.Fatalf("the finding quotes the value: %q", found[0])
	}
}

// An object_ref is a typed ID the core minted. It is never made from a string of the payload, which is a value the caller chose and which the reconstruction serves as written.
// The input gateway once wrote `req_` plus the client's request ID without its dashes (noema#466); this test finds that shape in any stored Record.
func TestAnObjectRefIsNeverDerivedFromThePayload(t *testing.T) {
	w := newWorld(t, "ten_aa")
	testpg.AllowUnguardedRecord(t, w.dsn) // this test stores the derived ref on purpose, to prove that the scan finds it
	derived := payloadEvent("ten_aa", 1, map[string]any{"request_id": "0b9a8c4e-6d1f-4e0a-9a2e-3c1d5f7b8a90"})
	derived.ObjectRef = "req_0b9a8c4e6d1f4e0a9a2e3c1d5f7b8a90"
	appendUnguarded(t, w, derived)
	found := scan(t, w)
	if len(found) != 1 || !strings.Contains(found[0], "object_ref") || !strings.Contains(found[0], "/request_id") {
		t.Fatalf("the scan found %v, want the derived object_ref named with the payload path", found)
	}
}

// The normal case is not a finding: an event whose payload cites its own object_ref, or another typed ID, verbatim.
func TestAnObjectRefThePayloadCitesVerbatimIsNotDerived(t *testing.T) {
	w := newWorld(t, "ten_aa")
	ev := payloadEvent("ten_aa", 1, map[string]any{"grant_id": "grt_0001", "self": "grt_0001", "other": "dec_0123456789ABCDEFGHJKMNPQRS"})
	w.append(t, ev)
	if found := scan(t, w); len(found) != 0 {
		t.Fatalf("the scan found %v", found)
	}
}

// appendUnguarded stores ev through recordsig.AppendUnguarded in its own transaction and commits it.
func appendUnguarded(t *testing.T, w *world, ev chain.Event) chain.Stored {
	t.Helper()
	tx, err := w.writer(t, ev.TenantID, ev.WrittenBy).Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	st := recordsig.AppendUnguarded(t, w.app, tx, ev)
	if err := tx.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
	return st
}
