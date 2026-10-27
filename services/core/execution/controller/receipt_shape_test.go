package controller_test

import (
	"regexp"
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/controller"
)

// contracts/schemas/execution-receipt.v1.json: the signed document carries schema_version and correlation_id, and
// transport_result is the class only (^[A-Z][A-Z0-9_]*$); the raw status travels beside it (REQ-EXE-007), unsigned.
func TestReceiptDocumentHasTheContractShape(t *testing.T) {
	w, c, g := setup(t)
	out, err := c.Execute(ctx(t), TenantID, g.Grant.GrantID)
	if err != nil || out.Receipt == nil {
		t.Fatalf("%+v %v", out, err)
	}
	doc := out.Receipt.Document()
	if doc["schema_version"] != "1.0" || doc["correlation_id"] != "corr_sim01" {
		t.Fatalf("document %v lacks schema_version or correlation_id", doc)
	}
	tr, _ := doc["transport_result"].(string)
	if !regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`).MatchString(tr) {
		t.Fatalf("transport_result %q does not match the contract", tr)
	}
	if _, has := doc["raw_code"]; has {
		t.Fatal("the raw code must not be part of the signed document")
	}
	_ = w
}

// decision-grant.v1 requires correlation_id; a grant without one cannot be traced through the Record, so it is void.
func TestGrantWithoutACorrelationIDIsVoid(t *testing.T) {
	w, c, g := setup(t)
	g.Grant.CorrelationID = ""
	g = w.Resign(g)
	w.Store(g)
	out, err := c.Execute(ctx(t), TenantID, g.Grant.GrantID)
	if err != nil || out.Void != controller.VoidBindingMismatch {
		t.Fatalf("outcome %+v err %v, want BINDING_MISMATCH", out, err)
	}
}

// A grant is addressed to one tenant (decision-grant.v1 tenant_id); a grant for another tenant is void (REQ-SEC-004).
func TestGrantForAnotherTenantIsVoid(t *testing.T) {
	w, c, g := setup(t)
	g.Grant.TenantID = "ten_other"
	g = w.Resign(g)
	w.Store(g)
	out, err := c.Execute(ctx(t), TenantID, g.Grant.GrantID)
	if err != nil || out.Void != controller.VoidBindingMismatch {
		t.Fatalf("outcome %+v err %v, want BINDING_MISMATCH", out, err)
	}
}
