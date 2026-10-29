package ledger_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/controller"
)

// PERD v1.18 REQ-EXE-004, EXE-04 (founder's decision of 28 Oct): the Controller verifies the receipt's HMAC before the Ledger
// writes it. With the real Ledger over PostgreSQL: a forged or empty signature writes nothing (no receipt, no Record event,
// no RecoveryRequest, no outbox event, the grant not voided), for an acknowledged answer and for a stale-version refusal; the
// attempt stays DISPATCHING and the sweeper, with the honest ring, takes it over as OUTCOME_UNKNOWN.

type forgingKeys struct {
	inner controller.KeyVerifier
	forge func(real string) string
}

func (f forgingKeys) VerifyWith(keyID string, msg []byte, sig string) bool {
	return f.inner.VerifyWith(keyID, msg, sig)
}

func (f forgingKeys) SignWith(keyID string, msg []byte) (string, error) {
	real, err := f.inner.SignWith(keyID, msg)
	return f.forge(real), err
}

func TestAReceiptWithAForgedOrEmptySignatureIsNotWrittenAndTheGrantIsNotVoided(t *testing.T) {
	forgeries := map[string]func(string) string{
		"empty":       func(string) string { return "" },
		"not hex":     func(string) string { return strings.Repeat("zz", 32) },
		"tampered":    func(real string) string { return real[:63] + map[bool]string{true: "1", false: "0"}[real[63] == '0'] },
		"upper case":  strings.ToUpper,
		"one short":   func(real string) string { return real[:63] },
		"well-formed": nil, // filled below: 64 lower-case hex characters that are the HMAC of nothing
	}
	answers := map[string]controller.TransportResult{
		"acknowledged":  {Class: controller.TransportAck2xx, ExternalAck: "CASE-1", RawCode: "201"},
		"stale version": {Class: controller.TransportVersionConflict, RawCode: "409:VERSION_MISMATCH"},
	}
	for fname, forge := range forgeries {
		for aname, answer := range answers {
			t.Run(fname+" signature, "+aname, func(t *testing.T) {
				w := newWorld(t)
				w.target.answer = answer
				if forge == nil {
					other := strings.Repeat("ab", 32)
					forge = func(string) string { return other }
				}
				w.ctl.Keys = forgingKeys{inner: w.keys, forge: forge}
				g := w.issue(t, 1)
				_, err := w.ctl.Execute(ctx(t), tenant, g.GrantID)
				if !errors.Is(err, controller.ErrReceiptSignature) {
					t.Fatalf("Execute: %v, want ErrReceiptSignature", err)
				}
				if w.target.count() != 1 {
					t.Fatalf("%d dispatches, want 1 (the write happened before the receipt)", w.target.count())
				}
				for q, want := range map[string]int{
					`SELECT count(*) FROM execution_receipt`:                                                    0,
					`SELECT count(*) FROM record_event WHERE event_type IN ('RECEIPT', 'GRANT_VOIDED')`:         0,
					`SELECT count(*) FROM recovery_request`:                                                     0,
					`SELECT count(*) FROM outbox_event WHERE topic IN ('outcome.verify', 'recovery.requested')`: 0,
					`SELECT count(*) FROM decision_grant WHERE status = 'SPENT'`:                                1,
					`SELECT count(*) FROM idempotency_record WHERE status = 'DISPATCHING'`:                      1,
				} {
					if got := w.count(t, q); got != want {
						t.Fatalf("%s = %d, want %d", q, got, want)
					}
				}
				// the sweeper has the same check
				if n, err := w.ctl.Recover(ctx(t), tenant); !errors.Is(err, controller.ErrReceiptSignature) || n != 0 {
					t.Fatalf("Recover with the forging signer: %d, %v; want 0 and ErrReceiptSignature", n, err)
				}
				if got := w.count(t, `SELECT count(*) FROM execution_receipt`); got != 0 {
					t.Fatalf("the sweeper wrote %d receipts with a forged signature", got)
				}
				// with the honest ring the attempt is taken over as OUTCOME_UNKNOWN and the grant stays SPENT
				w.ctl.Keys = w.keys
				if n, err := w.ctl.Recover(ctx(t), tenant); err != nil || n != 1 {
					t.Fatalf("Recover with the honest ring: %d, %v; want 1", n, err)
				}
				if got := w.count(t, `SELECT count(*) FROM execution_receipt WHERE execution_state = 'OUTCOME_UNKNOWN'`); got != 1 {
					t.Fatalf("%d OUTCOME_UNKNOWN receipts, want 1", got)
				}
				if got := w.count(t, `SELECT count(*) FROM decision_grant WHERE status = 'SPENT'`); got != 1 {
					t.Fatal("the grant is no longer SPENT")
				}
				verifyChain(t, w)
			})
		}
	}
}

// PERD v1.18 REQ-SEC-005, EC-X09, TM-17, §32.2; DEC-2026-10-28 §7 (D4), with the real Ledger over PostgreSQL. A failed
// verification appends exactly one SECURITY_EVENT SIGNATURE_INVALID, in its own transaction, written by Execution through
// record_insert, and nothing else changes. The event names the receipt, the attempt and the cause, never the signature.
func TestAFailedReceiptVerificationAppendsExactlyOneSecurityEventAndNothingElse(t *testing.T) {
	answers := map[string]controller.TransportResult{
		"acknowledged":  {Class: controller.TransportAck2xx, ExternalAck: "CASE-1", RawCode: "201"},
		"stale version": {Class: controller.TransportVersionConflict, RawCode: "409:VERSION_MISMATCH"},
	}
	for aname, answer := range answers {
		t.Run(aname, func(t *testing.T) {
			w := newWorld(t)
			w.target.answer = answer
			const forged = "not-a-signature-and-must-not-be-recorded"
			w.ctl.Keys = forgingKeys{inner: w.keys, forge: func(string) string { return forged }}
			g := w.issue(t, 1)
			if _, err := w.ctl.Execute(ctx(t), tenant, g.GrantID); !errors.Is(err, controller.ErrReceiptSignature) {
				t.Fatalf("Execute: %v, want ErrReceiptSignature", err)
			}
			// the events of E7 and E8 stay; the one new event is the SECURITY_EVENT, and there is no RECEIPT
			if got, want := w.events(t), []string{"REVALIDATION_PASSED", "GRANT_SPENT", "DISPATCH_ATTEMPTED", "SECURITY_EVENT"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("Record %v, want %v", got, want)
			}
			var kind, writtenBy, objectRef, cause, receiptID string
			var attempt int64
			if err := w.admin.QueryRow(ctx(t), `SELECT body->>'kind', written_by, object_ref, payload->>'cause', payload->>'receipt_id', (payload->>'attempt')::bigint
				FROM record_event WHERE tenant_id = $1 AND event_type = 'SECURITY_EVENT'`, tenant).Scan(&kind, &writtenBy, &objectRef, &cause, &receiptID, &attempt); err != nil {
				t.Fatal(err)
			}
			if kind != "SIGNATURE_INVALID" || writtenBy != "core-execution" || objectRef != g.GrantID || cause != controller.CauseSignatureNotVerified || receiptID == "" || attempt != 1 {
				t.Fatalf("event: kind %q, written_by %q, object_ref %q, cause %q, receipt %q, attempt %d", kind, writtenBy, objectRef, cause, receiptID, attempt)
			}
			if got := w.count(t, `SELECT count(*) FROM record_event WHERE payload::text LIKE '%' || $1 || '%' OR body::text LIKE '%' || $1 || '%'`, forged); got != 0 {
				t.Fatal("the signature value is in the Record")
			}
			for q, want := range map[string]int{
				`SELECT count(*) FROM execution_receipt`:                                                    0,
				`SELECT count(*) FROM record_event WHERE event_type IN ('RECEIPT', 'GRANT_VOIDED')`:         0,
				`SELECT count(*) FROM recovery_request`:                                                     0,
				`SELECT count(*) FROM outbox_event WHERE topic IN ('outcome.verify', 'recovery.requested')`: 0,
				`SELECT count(*) FROM decision_grant WHERE status = 'SPENT'`:                                1,
				`SELECT count(*) FROM idempotency_record WHERE status = 'DISPATCHING'`:                      1,
			} {
				if got := w.count(t, q); got != want {
					t.Fatalf("%s = %d, want %d", q, got, want)
				}
			}
			// the sweeper's failed verification is a second event, and the honest sweeper adds none
			if _, err := w.ctl.Recover(ctx(t), tenant); !errors.Is(err, controller.ErrReceiptSignature) {
				t.Fatalf("Recover with the forging signer: %v", err)
			}
			if got := w.count(t, `SELECT count(*) FROM record_event WHERE event_type = 'SECURITY_EVENT'`); got != 2 {
				t.Fatalf("%d SECURITY_EVENT after the sweeper's failure, want 2", got)
			}
			w.ctl.Keys = w.keys
			if n, err := w.ctl.Recover(ctx(t), tenant); err != nil || n != 1 {
				t.Fatalf("Recover with the honest ring: %d, %v; want 1", n, err)
			}
			if got := w.count(t, `SELECT count(*) FROM record_event WHERE event_type = 'SECURITY_EVENT'`); got != 2 {
				t.Fatalf("%d SECURITY_EVENT after the honest recovery, want 2", got)
			}
			verifyChain(t, w)
		})
	}
}

// EC-E16, REQ-AUD-001: the fault is real. A trigger planted with the admin role refuses a SECURITY_EVENT in the Record, so the
// append fails inside record_insert. Execute returns both failures; nothing else is written (no receipt, no event, no
// RecoveryRequest, no outbox event); the grant stays SPENT and the attempt DISPATCHING; the Record chain still verifies.
func TestIfTheSecurityEventCannotBeAppendedNothingIsWrittenAndBothFailuresAreReturned(t *testing.T) {
	w := newWorld(t)
	w.ctl.Keys = forgingKeys{inner: w.keys, forge: func(string) string { return "" }}
	g := w.issue(t, 1)
	for _, stmt := range []string{
		`CREATE FUNCTION synthetic_refuse_security_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
		   IF NEW.event_type = 'SECURITY_EVENT' THEN RAISE EXCEPTION 'synthetic fault: the Record refuses a security event'; END IF;
		   RETURN NEW; END $$`,
		`CREATE TRIGGER synthetic_refuse_security_event BEFORE INSERT ON record_event FOR EACH ROW EXECUTE FUNCTION synthetic_refuse_security_event()`,
	} {
		if _, err := w.admin.Exec(ctx(t), stmt); err != nil {
			t.Fatal(err)
		}
	}
	_, err := w.ctl.Execute(ctx(t), tenant, g.GrantID)
	if !errors.Is(err, controller.ErrReceiptSignature) || !strings.Contains(err.Error(), "synthetic fault") {
		t.Fatalf("Execute: %v, want ErrReceiptSignature together with the append failure", err)
	}
	if got, want := w.events(t), []string{"REVALIDATION_PASSED", "GRANT_SPENT", "DISPATCH_ATTEMPTED"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Record %v, want %v: the failed append left something behind", got, want)
	}
	for q, want := range map[string]int{
		`SELECT count(*) FROM execution_receipt`:                                                    0,
		`SELECT count(*) FROM recovery_request`:                                                     0,
		`SELECT count(*) FROM outbox_event WHERE topic IN ('outcome.verify', 'recovery.requested')`: 0,
		`SELECT count(*) FROM decision_grant WHERE status = 'SPENT'`:                                1,
		`SELECT count(*) FROM idempotency_record WHERE status = 'DISPATCHING'`:                      1,
	} {
		if got := w.count(t, q); got != want {
			t.Fatalf("%s = %d, want %d", q, got, want)
		}
	}
	verifyChain(t, w)
}
