package intentstore_test

// Trust's T1 verifies the admitted intent it reads back with the very check Save makes before it appends (A151, REQ-AUD-001, INV-17): Verify is that check, exported, so no
// second copy of the hash and binding rules exists. No database. SYNTHETIC.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/understanding/intentstore"
)

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:])
}

func goodAdmitted() intentstore.Admitted {
	doc := `{"correlation_id":"corr_c1","intent_id":"int_01J9X3","intent_node":"test.intent.investigate","status":"VALIDATED","supersedes":null,"tenant_id":"ten_aa","version":1}`
	return intentstore.Admitted{TenantID: "ten_aa", IntentID: "int_01J9X3", Version: 1, IntentNode: "test.intent.investigate", CorrelationID: "corr_c1", JSON: []byte(doc), IntentHash: sum([]byte(doc))}
}

func TestVerifyAcceptsABoundDocumentAndRefusesAnotherHash(t *testing.T) {
	if err := intentstore.Verify(goodAdmitted()); err != nil {
		t.Fatalf("the control failed: %v", err)
	}
	a := goodAdmitted()
	a.IntentHash = sum([]byte("{}"))
	if err := intentstore.Verify(a); !errors.Is(err, intentstore.ErrNotBound) {
		t.Errorf("a hash of another document: got %v, want ErrNotBound", err)
	}
	b := goodAdmitted()
	b.TenantID = "ten_bb"
	if err := intentstore.Verify(b); !errors.Is(err, intentstore.ErrNotBound) {
		t.Errorf("another tenant than the document's: got %v, want ErrNotBound", err)
	}
}
