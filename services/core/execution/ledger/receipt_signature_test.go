package ledger_test

import (
	"errors"
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
