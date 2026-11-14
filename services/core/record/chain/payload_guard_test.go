package chain_test

import (
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/record/chain"
)

// A184 (the Founder's ruling of 13 Nov, relayed), A154, REQ-AUD-003, REQ-SEC-003: the Record's append guard. A string in an event payload never holds a run of more than four
// digits (in any script), unless the whole string is a typed ID of the contract, an RFC 3339 timestamp or a content hash; a number-typed field is a number and is not examined. A
// refused payload writes nothing, and the error names the JSON path, never the value (the value may be account data).

func payloadEvent(tenant string, n int, payload any) chain.Event {
	e := event(tenant, n)
	e.Payload = payload
	return e
}

func appendErr(t *testing.T, w *world, ev chain.Event) error {
	t.Helper()
	tx, err := w.writer(t, ev.TenantID, ev.WrittenBy).Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.app.Append(ctx(t), tx, ev)
	if err != nil {
		_ = tx.Rollback(ctx(t))
		return err
	}
	if err := tx.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
	return nil
}

func TestAppendRefusesAPayloadStringWithADigitRunOverFour(t *testing.T) {
	w := newWorld(t, "ten_aa")
	for name, payload := range map[string]any{
		"a card number":                        map[string]any{"note": "paid with 4111111111111111"},
		"a card number in groups of four":      map[string]any{"note": "4111 1111 1111 1111"},
		"a card number with hyphens":           map[string]any{"note": "4111-1111-1111-1111"},
		"five digits":                          map[string]any{"note": "ref 12345"},
		"digits and letters mixed":             map[string]any{"note": "acct4111111111x"},
		"Arabic-Indic digits":                  map[string]any{"note": "٤١١١١١١١"},
		"full-width digits":                    map[string]any{"note": "４１１１１１"},
		"digits joined by zero-width marks":    map[string]any{"note": "4111​1111​1111"},
		"in a nested object":                   map[string]any{"a": map[string]any{"b": map[string]any{"c": "0123456789"}}},
		"in an array":                          map[string]any{"list": []any{"ok", "0123456789"}},
		"an ID-shaped value that is not an ID": map[string]any{"grant_id": "grt_4111111111111111!"},
		"a date that is not RFC 3339":          map[string]any{"on": "2026-11-13"},
		"a typed ID that is only digits":       map[string]any{"ref": "ten_4111111111111111"},
		"a bare number as the target":          map[string]any{"target_ref": "4111111111111111"},
		"a bare number as the acknowledgement": map[string]any{"external_ack": "4111111111111111"},
		"a number split by an algorithm name":  map[string]any{"note": "4111ed25519111ed25519111ed25519111"},
		"a number as an object key":            map[string]any{"4111111111111111": "x"},
	} {
		err := appendErr(t, w, payloadEvent("ten_aa", 1, payload))
		if err == nil {
			t.Errorf("%s: the append was accepted", name)
			continue
		}
		if !strings.Contains(strings.ToLower(err.Error()), "digit") {
			t.Errorf("%s: the refusal does not say why: %v", name, err)
		}
	}
}

// What the rule exempts, so that the guard does not stop a legitimate write: a run of four, typed IDs and hashes and nonces and signatures of the contracts, RFC 3339
// timestamps, number-typed fields (an amount in minor units), the key ID of a signature, the target's reference in a receipt, and a comma-grouped figure.
func TestAppendAcceptsWhatTheRuleExempts(t *testing.T) {
	w := newWorld(t, "ten_aa")
	hex64 := strings.Repeat("0123456789abcdef", 4)
	n := 0
	for name, payload := range map[string]any{
		"a run of four":          map[string]any{"note": "PIN 1234 and year 2026"},
		"typed IDs":              map[string]any{"grant_id": "grt_01234567890123456789AB2345", "int": "int_0123456789ab@2", "corr": "corr_synth0001"},
		"an RFC 3339 timestamp":  map[string]any{"at": "2026-11-13T10:12:00Z", "with": "2026-11-13T10:12:00.123+01:00"},
		"a content hash":         map[string]any{"h": "sha256:" + hex64, "k": "hmac-sha256:" + hex64},
		"the idempotency key":    map[string]any{"idempotency_key": "idk_v1_" + hex64},
		"the grant nonce":        map[string]any{"nonce": "n_0123456789abcdef0123456789abcdef"},
		"a signature":            map[string]any{"verdict": map[string]any{"sig": map[string]any{"key_id": "core-execution.ed25519.1", "value": hex64}}},
		"the target's reference": map[string]any{"external_ack": "CMP-2026-00001"},
		"a number-typed field":   map[string]any{"amount": 2000000000, "count": 12345678},
		"a comma-grouped figure": map[string]any{"note": "20,000,000"},
		"a code with digits":     map[string]any{"raw_status": "VERSION_CONFLICT_409_412:409"},
	} {
		n++
		if err := appendErr(t, w, payloadEvent("ten_aa", n, payload)); err != nil {
			t.Errorf("%s: refused: %v", name, err)
		}
	}
}

// A refused payload writes nothing: the chain is the same after it, so the next append extends the last good event and the chain verifies.
func TestARefusedPayloadLeavesTheChainAsItWas(t *testing.T) {
	w := newWorld(t, "ten_aa")
	first := w.append(t, event("ten_aa", 1))
	if err := appendErr(t, w, payloadEvent("ten_aa", 2, map[string]any{"note": "4111111111111111"})); err == nil {
		t.Fatal("the append was accepted")
	}
	second := w.append(t, event("ten_aa", 3))
	if second.Seq != first.Seq+1 || second.PrevHash != first.Hash {
		t.Fatalf("the event after a refusal does not extend the one before it: %+v after %+v", second, first)
	}
	var count int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM record_event`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("%d events in the Record (%v), want 2", count, err)
	}
}

// The refusal names the JSON path and the length of the run, never the value: the value may be account data, and an error is logged.
func TestTheRefusalNamesThePathAndNeverTheValue(t *testing.T) {
	w := newWorld(t, "ten_aa")
	err := appendErr(t, w, payloadEvent("ten_aa", 1, map[string]any{"outer": map[string]any{"note": "card 4111111111111111 held"}}))
	if err == nil {
		t.Fatal("the append was accepted")
	}
	msg := err.Error()
	if strings.Contains(msg, "4111") || strings.Contains(msg, "card") {
		t.Fatalf("the refusal quotes the payload: %q", msg)
	}
	if !strings.Contains(msg, "/outer/note") {
		t.Fatalf("the refusal does not name the path: %q", msg)
	}
}
