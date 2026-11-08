package outcomee2e_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"testing"

	"github.com/Noemai-org/noema/services/core/outcome/receiptsig"
	"github.com/Noemai-org/noema/services/core/outcome/verifier"
)

// INV-04, REQ-AUD-006, Founder decision of 4 Nov 2026: Outcome verifies the receipt's Ed25519 signature before it reads the
// target. A receipt that does not verify under core-execution's key gives UNKNOWN, a SECURITY_EVENT, and no read at all, even
// though the target really holds the case (US-06). The key is generated for the run and is SYNTHETIC.
func TestAReceiptThatDoesNotVerifyIsNeverProof(t *testing.T) {
	mc := startMockCase(t)
	s := newStack(t, mc)
	plan, err := os.ReadFile(faultFixture)
	if err != nil {
		t.Fatal(err)
	}
	s.bench.loadPlan(t, plan)
	rc := s.dispatch(t)
	s.bench.advance(t, "after_receipt")

	other, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s.pubs[receiptKeyID] = receiptsig.PublicKey{Key: other, Status: receiptsig.Active} // as if the receipt were forged

	res, err := s.verify(t, rc.ReceiptID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != verifier.Unknown || res.Reason != verifier.ReasonReceiptUnverifiable {
		t.Fatalf("verdict %s/%s, want UNKNOWN/RECEIPT_UNVERIFIABLE although the target holds the case", res.Verdict, res.Reason)
	}
	if n := s.count(t, `SELECT count(*) FROM record_event WHERE event_type = 'SECURITY_EVENT' AND body->>'kind' = 'SIGNATURE_INVALID'`); n != 1 {
		t.Fatalf("SIGNATURE_INVALID events = %d, want 1", n)
	}
	if n := s.count(t, `SELECT count(*) FROM record_event WHERE event_type = 'VERIFICATION_READ'`); n != 0 {
		t.Fatalf("%d reads of the target before the receipt was verified", n)
	}
	if n := s.count(t, `SELECT count(*) FROM outcome_verdict WHERE verdict = 'ACHIEVED'`); n != 0 {
		t.Fatalf("%d ACHIEVED verdicts for a receipt that did not verify", n)
	}
}
