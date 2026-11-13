package intentstore

// Review P1 on noema#471: what the store writes is bound to itself. The document is the canonical (RFC 8785) form that the hash was taken over, the hash is the
// document's, and the row's identity, version, correlation, node and status are the document's, all checked before anything is appended to the Record (REQ-AUD-001,
// INV-17, EC-E16). No database. SYNTHETIC.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func good() Admitted {
	doc := `{"correlation_id":"corr_c1","intent_id":"int_01J9X3","intent_node":"test.intent.investigate","status":"VALIDATED","supersedes":null,"tenant_id":"ten_aa","version":1}`
	return Admitted{TenantID: "ten_aa", IntentID: "int_01J9X3", Version: 1, IntentNode: "test.intent.investigate", CorrelationID: "corr_c1", JSON: []byte(doc), IntentHash: hashOf([]byte(doc))}
}

func TestADocumentThatIsBoundToItsHashAndItsRowPasses(t *testing.T) {
	if err := verify(good()); err != nil {
		t.Fatalf("the control failed: %v", err)
	}
	v2 := good()
	v2.Version, v2.Supersedes = 2, "int_01J9X3@1"
	v2.JSON = []byte(`{"correlation_id":"corr_c1","intent_id":"int_01J9X3","intent_node":"test.intent.investigate","status":"VALIDATED","supersedes":"int_01J9X3@1","tenant_id":"ten_aa","version":2}`)
	v2.IntentHash = hashOf(v2.JSON)
	if err := verify(v2); err != nil {
		t.Fatalf("version 2: %v", err)
	}
}

func TestADocumentThatTheHashOrTheRowDoNotDescribeIsRefused(t *testing.T) {
	withDoc := func(doc string) Admitted { a := good(); a.JSON = []byte(doc); a.IntentHash = hashOf(a.JSON); return a }
	base := `{"correlation_id":"corr_c1","intent_id":"int_01J9X3","intent_node":"test.intent.investigate","status":"VALIDATED","supersedes":null,"tenant_id":"ten_aa","version":1}`
	cases := map[string]Admitted{
		"a hash of another document":                 func() Admitted { a := good(); a.IntentHash = hashOf([]byte("{}")); return a }(),
		"a hash that is all zeros":                   func() Admitted { a := good(); a.IntentHash = "sha256:" + strings.Repeat("0", 64); return a }(),
		"a document with a space":                    withDoc(strings.Replace(base, `"version":1`, `"version": 1`, 1)),
		"members out of order":                       withDoc(`{"intent_id":"int_01J9X3","correlation_id":"corr_c1","intent_node":"test.intent.investigate","status":"VALIDATED","supersedes":null,"tenant_id":"ten_aa","version":1}`),
		"a fractional number":                        withDoc(strings.Replace(base, `"version":1`, `"version":1,"x":0.5`, 1)),
		"trailing data":                              withDoc(base + ` {}`),
		"not an object":                              withDoc(`[1]`),
		"another intent id":                          func() Admitted { a := good(); a.IntentID = "int_other"; return a }(),
		"another tenant":                             func() Admitted { a := good(); a.TenantID = "ten_bb"; return a }(),
		"another correlation":                        func() Admitted { a := good(); a.CorrelationID = "corr_c2"; return a }(),
		"another node":                               func() Admitted { a := good(); a.IntentNode = "test.intent.other"; return a }(),
		"another version":                            func() Admitted { a := good(); a.Version = 2; return a }(),
		"a supersedes the document lacks":            func() Admitted { a := good(); a.Supersedes = "int_01J9X3@0"; return a }(),
		"a document with a supersedes the row lacks": withDoc(strings.Replace(base, `"supersedes":null`, `"supersedes":"int_01J9X3@1"`, 1)),
		"a status that is not VALIDATED":             withDoc(strings.Replace(base, `"VALIDATED"`, `"EXTRACTED"`, 1)),
		"a version that is a string":                 withDoc(strings.Replace(base, `"version":1`, `"version":"1"`, 1)),
		"an intent id that is a number":              withDoc(strings.Replace(base, `"int_01J9X3"`, `1`, 1)),
	}
	for name, a := range cases {
		err := verify(a)
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if !errors.Is(err, ErrNotBound) {
			t.Errorf("%s: %v is not ErrNotBound", name, err)
		}
		if strings.Contains(err.Error(), "int_01J9X3") || strings.Contains(err.Error(), "ten_aa") {
			t.Errorf("%s: the error quotes the document: %v", name, err)
		}
	}
}
