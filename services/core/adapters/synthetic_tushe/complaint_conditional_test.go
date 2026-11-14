package synthetic_tushe

// The conditional complaint create (Architect ruling A185, noema#522; mock-case noema#537): a complaint log that must be made only if the case has none sends
// `If-None-Match: *`, and a `412 PRECONDITION_FAILED` answer is a refused create-if-absent, never a stale version and never an acknowledgement. The answer names the complaint that
// exists, so the caller can read it back; it is not adopted as this write's result. This file tests the adapter against the documented answer shape; the real mock-case is exercised
// in adapters/outcome_e2e (records_test.go), against PostgreSQL.

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
)

func complaintServer(t *testing.T, status int, body string, seen *http.Header) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func conditional(key string) adapters.LogComplaintInput {
	return adapters.LogComplaintInput{IdempotencyKey: key, TenantID: "ten_a", CaseID: "case_1", Timeout: 2 * time.Second, IfNone: true}
}

const refused412 = `{"error":{"code":"PRECONDITION_FAILED","message":"a complaint reference already exists for this case: the create was conditional on there being none","retryable":false,` +
	`"details":[{"field":"existing_complaint_ref","issue":"CPR-2026-00001"}]},"correlation_id":"corr_x"}`

func TestAConditionalComplaintSendsIfNoneMatch(t *testing.T) {
	var seen http.Header
	cli := newClient(t, complaintServer(t, 412, refused412, &seen))
	cli.LogComplaint(deadline(t), conditional("k1"))
	if seen.Get("If-None-Match") != "*" || seen.Get("Idempotency-Key") != "k1" {
		t.Fatalf("headers sent: %v", seen)
	}
}

func TestAnUnconditionalComplaintSendsNoPrecondition(t *testing.T) {
	var seen http.Header
	cli := newClient(t, complaintServer(t, 412, refused412, &seen))
	in := conditional("k1")
	in.IfNone = false
	cli.LogComplaint(deadline(t), in)
	if _, sent := seen["If-None-Match"]; sent {
		t.Fatalf("an unconditional complaint must not send If-None-Match: %v", seen)
	}
}

func TestARefusedConditionalComplaintIsADuplicateCreateThatNamesTheExistingOne(t *testing.T) {
	var seen http.Header
	cli := newClient(t, complaintServer(t, 412, refused412, &seen))
	r := cli.LogComplaint(deadline(t), conditional("k2"))
	if r.Class != adapters.ClassDuplicateCreate || r.ExistingComplaintRef != "CPR-2026-00001" || r.RawCode != "412:PRECONDITION_FAILED" {
		t.Fatalf("a refused create-if-absent: %+v", r)
	}
	if r.ExternalAck != "" || r.Complaint != nil || r.CurrentVersionToken != "" {
		t.Fatalf("the existing complaint must be named, never adopted as this write's result, and a create has no version token: %+v", r)
	}
	if r.Class == adapters.ClassVersionConflict {
		t.Fatal("a 412 on a create is not a stale version")
	}
}

func TestAnUnexpectedBodyOn412IsAPlainRejectionAndNeverAnAcknowledgementOrADuplicateCreate(t *testing.T) {
	for name, body := range map[string]string{
		"another code": `{"error":{"code":"VERSION_MISMATCH","message":"x","retryable":false}}`,
		"not json":     `<html>`,
		"empty":        ``,
	} {
		var seen http.Header
		cli := newClient(t, complaintServer(t, 412, body, &seen))
		if r := cli.LogComplaint(deadline(t), conditional("k3")); r.Class != adapters.ClassRejected4xx || r.ExistingComplaintRef != "" {
			t.Errorf("%s: %+v", name, r)
		}
	}
}

func TestA412WithNoExistingReferenceIsStillADuplicateCreateAndNamesNothing(t *testing.T) {
	var seen http.Header
	cli := newClient(t, complaintServer(t, 412, `{"error":{"code":"PRECONDITION_FAILED","message":"x","retryable":false}}`, &seen))
	r := cli.LogComplaint(deadline(t), conditional("k4"))
	if r.Class != adapters.ClassDuplicateCreate || r.ExistingComplaintRef != "" {
		t.Fatalf("%+v", r)
	}
}
