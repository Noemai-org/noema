package outcomee2e_test

// EXE-05, A157, REQ-EXE-003, REQ-EXE-006, REQ-EXE-007, REQ-ENV-008, INV-04: the core's case client appends evidence and logs a complaint reference
// on the REAL mock-case (real handlers, real PostgreSQL), reads each back, and is refused where the target refuses: a replay is the original, the
// same key with another payload is a collision, a case of another tenant does not exist, and a find is scoped. SYNTHETIC.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/adapters/synthetic_tushe"
)

func sha(p string) string { s := sha256.Sum256([]byte(p)); return "sha256:" + hex.EncodeToString(s[:]) }

func TestEvidenceAndComplaintWritesRunAgainstTheRealMockCase(t *testing.T) {
	mc := startMockCase(t)
	c, err := synthetic_tushe.NewCaseClient(mc.URL, mc.TokenFile, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	mk := func(tenant, tx string) string {
		r := c.CreateCase(ctx, adapters.CreateCaseInput{IdempotencyKey: "case-" + tenant + tx, TenantID: tenant, PrincipalID: "usr_1", TransactionRef: tx, ReasonCode: "CREDIT_MISSING", CreatedByIntent: "int_01J9X3", Epoch: 0, Timeout: 5 * time.Second})
		if r.Class != adapters.ClassAck2xx || r.Case == nil {
			t.Fatalf("create case: %s %s", r.Class, r.RawCode)
		}
		return r.Case.CaseID
	}
	caseA, caseB := mk("ten_a", "TX1"), mk("ten_b", "TX2")
	body := `{"note":"card used at 14:02","amount_minor":150000}`

	// Evidence: created, then a replay of the same key and payload is the original.
	ev := adapters.AppendEvidenceInput{IdempotencyKey: "ev-1", TenantID: "ten_a", CaseID: caseA, Payload: body, Timeout: 5 * time.Second}
	first := c.AppendEvidence(ctx, ev)
	if first.Class != adapters.ClassAck2xx || first.RawCode != "201" || first.Evidence == nil || first.Evidence.PayloadHash != sha(body) || first.ExternalAck == "" {
		t.Fatalf("append: %+v", first)
	}
	replay := c.AppendEvidence(ctx, ev)
	if replay.Class != adapters.ClassAck2xx || replay.RawCode != "200:REPLAY" || replay.ExternalAck != first.ExternalAck {
		t.Fatalf("replay: %+v", replay)
	}
	// The same key with another payload is a collision, not an acknowledgement (EC-E06).
	other := ev
	other.Payload = `{"note":"another"}`
	if r := c.AppendEvidence(ctx, other); r.Class != adapters.ClassRejected4xx || !r.KeyCollision {
		t.Fatalf("collision: %+v", r)
	}
	// A case of another tenant does not exist for this tenant (INV-12, REQ-SEC-004).
	cross := ev
	cross.IdempotencyKey, cross.CaseID = "ev-cross", caseB
	if r := c.AppendEvidence(ctx, cross); r.Class != adapters.ClassRejected4xx || r.RawCode != "404:NOT_FOUND" {
		t.Fatalf("cross-tenant append: %+v", r)
	}

	// Read-back by ID and by key; the stored bytes are the bytes sent.
	got, err := c.GetEvidence(ctx, "ten_a", first.ExternalAck)
	if err != nil || got.Payload != body || got.PayloadHash != sha(body) || got.CaseID != caseA {
		t.Fatalf("get evidence: %+v %v", got, err)
	}
	if _, err := c.GetEvidence(ctx, "ten_b", first.ExternalAck); err == nil {
		t.Fatal("another tenant read this tenant's attachment")
	} else if re, ok := err.(*adapters.ReadError); !ok || re.Failure != adapters.ReadNotFound {
		t.Fatalf("cross-tenant read: %v", err)
	}
	pg, err := c.FindEvidence(ctx, adapters.RecordQuery{TenantID: "ten_a", IdempotencyKey: "ev-1"})
	if err != nil || !pg.Complete || len(pg.Items) != 1 || pg.Items[0].AttachmentID != first.ExternalAck {
		t.Fatalf("find evidence by key: %+v %v", pg, err)
	}
	if pg, err := c.FindEvidence(ctx, adapters.RecordQuery{TenantID: "ten_a", IdempotencyKey: "never"}); err != nil || !pg.Complete || len(pg.Items) != 0 {
		t.Fatalf("absence must be a complete empty page: %+v %v", pg, err)
	}

	// Complaints: logged, replayed, collided, scoped, read back.
	cm := adapters.LogComplaintInput{IdempotencyKey: "cm-1", TenantID: "ten_a", CaseID: caseA, Timeout: 5 * time.Second}
	logged := c.LogComplaint(ctx, cm)
	if logged.Class != adapters.ClassAck2xx || logged.RawCode != "201" || logged.Complaint == nil || logged.ExternalAck == "" {
		t.Fatalf("complaint: %+v", logged)
	}
	if r := c.LogComplaint(ctx, cm); r.RawCode != "200:REPLAY" || r.ExternalAck != logged.ExternalAck {
		t.Fatalf("complaint replay: %+v", r)
	}
	moved := cm
	moved.CaseID = caseA + "x"
	if r := c.LogComplaint(ctx, moved); r.Class != adapters.ClassRejected4xx {
		t.Fatalf("complaint on an unknown case: %+v", r)
	}
	gc, err := c.GetComplaint(ctx, "ten_a", logged.ExternalAck)
	if err != nil || gc.CaseID != caseA || gc.ClockStartedAt == "" {
		t.Fatalf("get complaint: %+v %v", gc, err)
	}
	cp, err := c.FindComplaints(ctx, adapters.RecordQuery{TenantID: "ten_a", CaseID: caseA})
	if err != nil || !cp.Complete || len(cp.Items) != 1 {
		t.Fatalf("find complaints by case: %+v %v", cp, err)
	}
	if cp, err := c.FindComplaints(ctx, adapters.RecordQuery{TenantID: "ten_b", CaseID: caseA}); err != nil || len(cp.Items) != 0 {
		t.Fatalf("another tenant's find returned %d item(s): %v", len(cp.Items), err)
	}
}
