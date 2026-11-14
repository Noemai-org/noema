package outcomee2e_test

// A185, noema#522, REQ-EXE-004, REQ-EXE-007, INV-03: the core's case client logs a complaint reference CONDITIONALLY on the REAL mock-case (noema#537: a per-case lock and a unique
// partial index): the first log is acknowledged; a rival under another key is a refused create-if-absent (ClassDuplicateCreate) that names the complaint that exists and writes
// nothing; the first key repeated is its replay; and eight concurrent logs under distinct keys give exactly one acknowledgement and seven refusals, one complaint in the target.
// SYNTHETIC. Needs PostgreSQL: CI is its evidence.

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/adapters/synthetic_tushe"
)

func TestAConditionalComplaintIsLoggedOnceOnTheRealMockCase(t *testing.T) {
	mc := startMockCase(t)
	c, err := synthetic_tushe.NewCaseClient(mc.URL, mc.TokenFile, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	made := c.CreateCase(ctx, adapters.CreateCaseInput{IdempotencyKey: "case-1", TenantID: "ten_a", PrincipalID: "usr_1", TransactionRef: "TX1", ReasonCode: "CREDIT_MISSING", CreatedByIntent: "int_1", Timeout: 5 * time.Second})
	if made.Class != adapters.ClassAck2xx || made.Case == nil {
		t.Fatalf("create case: %+v", made)
	}
	caseID := made.Case.CaseID
	log := func(key string) adapters.WriteResult {
		return c.LogComplaint(ctx, adapters.LogComplaintInput{IdempotencyKey: key, TenantID: "ten_a", CaseID: caseID, Timeout: 5 * time.Second, IfNone: true})
	}

	first := log("cm-1")
	if first.Class != adapters.ClassAck2xx || first.RawCode != "201" || first.ExternalAck == "" {
		t.Fatalf("the first conditional log: %+v", first)
	}
	rival := log("cm-2")
	if rival.Class != adapters.ClassDuplicateCreate || rival.ExistingComplaintRef != first.ExternalAck || rival.ExternalAck != "" || rival.Complaint != nil {
		t.Fatalf("a rival under another key must be a refused create-if-absent naming the complaint that exists: %+v (first %s)", rival, first.ExternalAck)
	}
	if again := log("cm-1"); again.Class != adapters.ClassAck2xx || again.RawCode != "200:REPLAY" || again.ExternalAck != first.ExternalAck {
		t.Fatalf("the first key repeated is its replay: %+v", again)
	}
	page, err := c.FindComplaints(ctx, adapters.RecordQuery{TenantID: "ten_a", CaseID: caseID})
	if err != nil || !page.Complete || len(page.Items) != 1 {
		t.Fatalf("exactly one complaint exists for the case: %+v %v", page, err)
	}
}

func TestEightConcurrentConditionalComplaintsLogExactlyOneOnTheRealMockCase(t *testing.T) {
	mc := startMockCase(t)
	c, err := synthetic_tushe.NewCaseClient(mc.URL, mc.TokenFile, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	made := c.CreateCase(ctx, adapters.CreateCaseInput{IdempotencyKey: "case-1", TenantID: "ten_a", PrincipalID: "usr_1", TransactionRef: "TX1", ReasonCode: "CREDIT_MISSING", CreatedByIntent: "int_1", Timeout: 5 * time.Second})
	if made.Class != adapters.ClassAck2xx || made.Case == nil {
		t.Fatalf("create case: %+v", made)
	}
	const callers = 8
	results := make([]adapters.WriteResult, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = c.LogComplaint(ctx, adapters.LogComplaintInput{IdempotencyKey: fmt.Sprintf("race-%d", i), TenantID: "ten_a", CaseID: made.Case.CaseID, Timeout: 10 * time.Second, IfNone: true})
		}(i)
	}
	wg.Wait()
	acks, refusals, winner := 0, 0, ""
	for _, r := range results {
		switch r.Class {
		case adapters.ClassAck2xx:
			acks++
			winner = r.ExternalAck
		case adapters.ClassDuplicateCreate:
			refusals++
		default:
			t.Errorf("unexpected result: %+v", r)
		}
	}
	if acks != 1 || refusals != callers-1 {
		t.Fatalf("exactly one acknowledgement and %d refusals, got %d and %d", callers-1, acks, refusals)
	}
	for _, r := range results {
		if r.Class == adapters.ClassDuplicateCreate && r.ExistingComplaintRef != winner {
			t.Errorf("a refusal must name the winner %s: %+v", winner, r)
		}
	}
	page, err := c.FindComplaints(ctx, adapters.RecordQuery{TenantID: "ten_a", CaseID: made.Case.CaseID})
	if err != nil || !page.Complete || len(page.Items) != 1 {
		t.Fatalf("exactly one complaint exists: %+v %v", page, err)
	}
}
