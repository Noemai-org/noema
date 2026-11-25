package grants_test

// A574 and A577 (interim; REQ-TRU-001, INV-03, INV-17): a Decision Record of an outcome reached before the policy was evaluated holds a null policy_version, and it is still signed and verified (the null
// is part of the signed bytes); an ALLOW or a CONFIRM with no policy version is never signed, so no grant can rest on a record that names no policy. All values are SYNTHETIC.

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

func earlyRecord(t *testing.T) contracts.DecisionRecordV2 {
	t.Helper()
	raw, err := os.ReadFile("../../../../contracts/fixtures/valid/decision-record.v2/escalate_early_null_basis.json")
	if err != nil {
		t.Fatal(err)
	}
	var r contracts.DecisionRecordV2
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAnEarlyOutcomeWithANullBasisIsSignedAndVerified(t *testing.T) {
	k := keys(t)
	r := earlyRecord(t)
	if r.PolicyVersion != nil || r.EvidenceSnapshotId != nil || r.EvidenceFreshness != nil {
		t.Fatalf("the fixture is not an early record: %v %v %v", r.PolicyVersion, r.EvidenceSnapshotId, r.EvidenceFreshness)
	}
	sig, err := k.SignRecord(r)
	if err != nil {
		t.Fatalf("an early record with a null basis is not signed: %v", err)
	}
	r.Sig = sig
	if err := k.VerifyRecord(r); err != nil {
		t.Fatalf("a signed early record does not verify: %v", err)
	}
	r.PolicyVersion = strp("fin.pol@3.2") // the null is in the signed bytes: a policy added afterwards breaks the signature
	if err := k.VerifyRecord(r); err == nil {
		t.Error("a policy version added to a signed early record still verifies")
	}
}

func TestAnAllowOrAConfirmWithNoPolicyVersionIsNeverSigned(t *testing.T) {
	k := keys(t)
	for _, outcome := range []contracts.EnumsV1DecisionOutcome{contracts.EnumsV1DecisionOutcomeALLOW, contracts.EnumsV1DecisionOutcomeCONFIRM} {
		r := record(t)
		r.Outcome = outcome
		r.PolicyVersion = nil
		if sig, err := k.SignRecord(r); err == nil {
			t.Errorf("%s with a null policy version was signed (%+v)", outcome, sig)
		}
	}
}
