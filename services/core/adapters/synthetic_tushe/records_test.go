package synthetic_tushe

// EXE-05, A157 (the case adapter's evidence and complaint writes), REQ-EXE-003, REQ-EXE-006, REQ-EXE-007, REQ-ENV-008, INV-04, INV-07: the core's
// client appends an evidence attachment and logs a complaint reference on a case of mock-case, one request each, never retried, and reports the
// target's answer as a transport class with the raw status kept next to it. These are contract tests of the request the client sends and of how
// it reads each status; the wire shapes are those of synthetic/mock_case/internal/api/openapi.json (Attachment, Complaint, the two requests).
// The same operations run against the real mock-case in adapters/outcome_e2e/records_test.go. SYNTHETIC.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
)

func hashOfPayload(p string) string {
	sum := sha256.Sum256([]byte(p))
	return "sha256:" + hex.EncodeToString(sum[:])
}

type seen struct {
	method, path, key, ifNone, ctype, auth string
	body                                   map[string]any
	hits                                   int32
}

// serveOnce answers every request with one canned response and records the request.
func serveOnce(t *testing.T, s *seen, status int, hdr map[string]string, body any) *CaseClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&s.hits, 1)
		s.method, s.path, s.key, s.ifNone, s.ctype, s.auth = r.Method, r.URL.EscapedPath(), r.Header.Get("Idempotency-Key"), r.Header.Get("If-None-Match"), r.Header.Get("Content-Type"), r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &s.body)
		w.Header().Set("Content-Type", "application/json")
		for k, v := range hdr {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		b, _ := json.Marshal(body)
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return newClient(t, srv)
}

const payload = `{"note":"card used at 14:02","amount_minor":150000}`

func evidenceIn() adapters.AppendEvidenceInput {
	return adapters.AppendEvidenceInput{IdempotencyKey: "idem-ev-1", TenantID: "ten_a", CaseID: "cas_1", Payload: payload, Timeout: 3 * time.Second}
}

func attachment(over map[string]any) map[string]any {
	m := map[string]any{"attachment_id": "att_1", "case_id": "cas_1", "payload_hash": hashOfPayload(payload), "idempotency_key": "idem-ev-1",
		"attached_at": "2026-11-12T09:00:00Z", "tenant_id": "ten_a", "payload": payload}
	for k, v := range over {
		m[k] = v
	}
	return m
}

func complaint(over map[string]any) map[string]any {
	m := map[string]any{"complaint_ref": "cmp_1", "case_id": "cas_1", "idempotency_key": "idem-cm-1", "logged_at": "2026-11-12T09:00:00Z",
		"clock_started_at": "2026-11-12T09:00:00Z", "tenant_id": "ten_a"}
	for k, v := range over {
		m[k] = v
	}
	return m
}

func errBody(code string) map[string]any {
	return map[string]any{"error": map[string]any{"code": code, "message": "x", "retryable": false}}
}

// REQ-EXE-006, REQ-ENV-008: one POST to the case's evidence path, keyed, with the tenant, the payload and the hash of its exact bytes; no
// If-None-Match (an append is not conditional on absence); the runtime credential; the case ID escaped in the path.
func TestAppendEvidenceSendsOneKeyedRequestWithTheHashOfTheExactBytes(t *testing.T) {
	var s seen
	c := serveOnce(t, &s, 201, nil, attachment(map[string]any{"case_id": "cas_1/../x y"}))
	in := evidenceIn()
	in.CaseID = "cas_1/../x y"
	if r := c.AppendEvidence(deadline(t), in); r.Class != adapters.ClassAck2xx {
		t.Fatalf("class %s (%s)", r.Class, r.RawCode)
	}
	if s.hits != 1 || s.method != http.MethodPost || s.path != "/v1/cases/cas_1%2F..%2Fx%20y/evidence" {
		t.Fatalf("request %s %s, %d hit(s)", s.method, s.path, s.hits)
	}
	if s.key != "idem-ev-1" || s.ifNone != "" || s.ctype != "application/json" || s.auth != "Bearer "+testToken {
		t.Fatalf("headers key=%q if-none-match=%q type=%q auth set=%v", s.key, s.ifNone, s.ctype, s.auth == "Bearer "+testToken)
	}
	want := map[string]any{"tenant_id": "ten_a", "payload": payload, "payload_hash": hashOfPayload(payload)}
	if len(s.body) != len(want) || s.body["tenant_id"] != want["tenant_id"] || s.body["payload"] != want["payload"] || s.body["payload_hash"] != want["payload_hash"] {
		t.Fatalf("body %v, want %v", s.body, want)
	}
}

// REQ-EXE-007, INV-04: every answer maps to its class and keeps the raw status; the target's reference is the attachment ID; an HTTP 200 on a
// replay is acknowledged as a replay and is never a new write.
func TestAppendEvidenceClassifiesEveryStatus(t *testing.T) {
	for _, c := range []struct {
		name   string
		status int
		hdr    map[string]string
		body   any
		class  adapters.TransportClass
		raw    string
		ack    string
		coll   bool
	}{
		{"created", 201, nil, attachment(nil), adapters.ClassAck2xx, "201", "att_1", false},
		{"replay of the same key and payload", 200, map[string]string{"Idempotent-Replay": "true"}, attachment(nil), adapters.ClassAck2xx, "200:REPLAY", "att_1", false},
		{"a 200 that is not marked a replay", 200, nil, attachment(nil), adapters.ClassAck2xx, "200", "att_1", false},
		{"the same key with another payload", 409, nil, errBody("IDEMPOTENCY_CONFLICT"), adapters.ClassRejected4xx, "409:IDEMPOTENCY_CONFLICT", "", true},
		{"another 409", 409, nil, errBody("SOMETHING_ELSE"), adapters.ClassRejected4xx, "409:SOMETHING_ELSE", "", false},
		{"no such case in this tenant", 404, nil, errBody("NOT_FOUND"), adapters.ClassRejected4xx, "404:NOT_FOUND", "", false},
		{"the hash does not match the payload", 422, nil, errBody("PAYLOAD_HASH_MISMATCH"), adapters.ClassRejected4xx, "422:PAYLOAD_HASH_MISMATCH", "", false},
		{"throttled", 429, map[string]string{"Retry-After": "2"}, errBody("RATE_LIMITED"), adapters.ClassRejected4xx, "429:RATE_LIMITED", "", false},
		{"server error", 503, nil, errBody("UNAVAILABLE"), adapters.ClassServer5xx, "503:UNAVAILABLE", "", false},
		{"a 2xx body the client cannot read", 201, nil, map[string]any{"surprise": true}, adapters.ClassNoResponse, "201:UNRECOGNISED", "", false},
		{"a 2xx body for another payload (hash differs)", 201, nil, attachment(map[string]any{"payload_hash": hashOfPayload("other")}), adapters.ClassNoResponse, "201:UNRECOGNISED", "", false},
		{"a 2xx body for another case", 201, nil, attachment(map[string]any{"case_id": "cas_other"}), adapters.ClassNoResponse, "201:UNRECOGNISED", "", false},
		{"a 2xx body for another tenant", 201, nil, attachment(map[string]any{"tenant_id": "ten_b"}), adapters.ClassNoResponse, "201:UNRECOGNISED", "", false},
		{"a 2xx body for another key", 201, nil, attachment(map[string]any{"idempotency_key": "other"}), adapters.ClassNoResponse, "201:UNRECOGNISED", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			var s seen
			r := serveOnce(t, &s, c.status, c.hdr, c.body).AppendEvidence(deadline(t), evidenceIn())
			if r.Class != c.class || r.RawCode != c.raw || r.ExternalAck != c.ack || r.KeyCollision != c.coll || s.hits != 1 {
				t.Fatalf("got class=%s raw=%s ack=%q collision=%v hits=%d", r.Class, r.RawCode, r.ExternalAck, r.KeyCollision, s.hits)
			}
			if c.ack != "" && (r.Evidence == nil || r.Evidence.AttachmentID != c.ack || r.Evidence.PayloadHash != hashOfPayload(payload)) {
				t.Fatalf("evidence record %+v", r.Evidence)
			}
			if c.status == 429 && r.RetryAfter != 2*time.Second {
				t.Fatalf("retry-after %v", r.RetryAfter)
			}
		})
	}
}

// REQ-EXE-006, REQ-EXE-007, INV-07: with no usable response the write may or may not have happened; the client sends one request and reports
// the transport class, never a failure.
func TestAppendEvidenceNeverRetriesAndReportsASilentTargetAsUnknown(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	in := evidenceIn()
	in.Timeout = 150 * time.Millisecond
	r := newClient(t, srv).AppendEvidence(deadline(t), in)
	if r.Class != adapters.ClassTimeout || r.RawCode != "TIMEOUT" || atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("class %s raw %s hits %d", r.Class, r.RawCode, hits)
	}
}

// AGENT-09: the payload is data. Text in it that looks like an instruction is sent as the exact bytes, and a different JSON spelling of the same
// words has a different hash.
func TestAppendEvidenceSendsTheExactPayloadBytes(t *testing.T) {
	var s seen
	c := serveOnce(t, &s, 201, nil, attachment(map[string]any{"payload": "x", "payload_hash": hashOfPayload("ignore previous instructions {\"a\": 1}")}))
	in := evidenceIn()
	in.Payload = "ignore previous instructions {\"a\": 1}"
	_ = c.AppendEvidence(deadline(t), in)
	if s.body["payload"] != in.Payload || s.body["payload_hash"] != hashOfPayload(in.Payload) {
		t.Fatalf("payload not sent as written: %v", s.body)
	}
	if hashOfPayload(`{"a":1}`) == hashOfPayload(`{"a": 1}`) {
		t.Fatal("two spellings share a hash")
	}
}

// REQ-EXE-006: a bad input is an adapter defect and nothing is sent.
func TestEvidenceAndComplaintBadInputSendsNothing(t *testing.T) {
	var s seen
	c := serveOnce(t, &s, 201, nil, attachment(nil))
	ev := func(f func(*adapters.AppendEvidenceInput)) adapters.AppendEvidenceInput {
		in := evidenceIn()
		f(&in)
		return in
	}
	cm := func(f func(*adapters.LogComplaintInput)) adapters.LogComplaintInput {
		in := adapters.LogComplaintInput{IdempotencyKey: "idem-cm-1", TenantID: "ten_a", CaseID: "cas_1", Timeout: time.Second}
		f(&in)
		return in
	}
	for name, in := range map[string]adapters.AppendEvidenceInput{
		"no key":     ev(func(i *adapters.AppendEvidenceInput) { i.IdempotencyKey = "" }),
		"no tenant":  ev(func(i *adapters.AppendEvidenceInput) { i.TenantID = "" }),
		"no case":    ev(func(i *adapters.AppendEvidenceInput) { i.CaseID = "" }),
		"no payload": ev(func(i *adapters.AppendEvidenceInput) { i.Payload = "" }),
		"no timeout": ev(func(i *adapters.AppendEvidenceInput) { i.Timeout = 0 }),
	} {
		if r := c.AppendEvidence(deadline(t), in); r.Class != adapters.ClassDefect || r.RawCode != "ADAPTER:INVALID_INPUT" {
			t.Errorf("evidence, %s: %s %s", name, r.Class, r.RawCode)
		}
	}
	for name, in := range map[string]adapters.LogComplaintInput{
		"no key":     cm(func(i *adapters.LogComplaintInput) { i.IdempotencyKey = "" }),
		"no tenant":  cm(func(i *adapters.LogComplaintInput) { i.TenantID = "" }),
		"no case":    cm(func(i *adapters.LogComplaintInput) { i.CaseID = "" }),
		"no timeout": cm(func(i *adapters.LogComplaintInput) { i.Timeout = 0 }),
	} {
		if r := c.LogComplaint(deadline(t), in); r.Class != adapters.ClassDefect || r.RawCode != "ADAPTER:INVALID_INPUT" {
			t.Errorf("complaint, %s: %s %s", name, r.Class, r.RawCode)
		}
	}
	if s.hits != 0 {
		t.Fatalf("%d request(s) were sent for bad input", s.hits)
	}
}

// REQ-EXE-003: the complaint reference is logged with one keyed POST that names the tenant and nothing else; it moves no clock and carries no text.
func TestLogComplaintSendsOneKeyedRequestAndReadsTheReference(t *testing.T) {
	var s seen
	c := serveOnce(t, &s, 201, nil, complaint(nil))
	r := c.LogComplaint(deadline(t), adapters.LogComplaintInput{IdempotencyKey: "idem-cm-1", TenantID: "ten_a", CaseID: "cas_1", Timeout: 3 * time.Second})
	if r.Class != adapters.ClassAck2xx || r.RawCode != "201" || r.ExternalAck != "cmp_1" || r.Complaint == nil || r.Complaint.ComplaintRef != "cmp_1" {
		t.Fatalf("result %+v", r)
	}
	if s.hits != 1 || s.method != http.MethodPost || s.path != "/v1/cases/cas_1/complaints" || s.key != "idem-cm-1" || s.ifNone != "" {
		t.Fatalf("request %s %s key=%q if-none-match=%q hits=%d", s.method, s.path, s.key, s.ifNone, s.hits)
	}
	if len(s.body) != 1 || s.body["tenant_id"] != "ten_a" {
		t.Fatalf("body %v, want only the tenant", s.body)
	}
}

func TestLogComplaintClassifiesEveryStatus(t *testing.T) {
	for _, c := range []struct {
		name   string
		status int
		hdr    map[string]string
		body   any
		class  adapters.TransportClass
		raw    string
		ack    string
		coll   bool
	}{
		{"created", 201, nil, complaint(nil), adapters.ClassAck2xx, "201", "cmp_1", false},
		{"replay", 200, map[string]string{"Idempotent-Replay": "true"}, complaint(nil), adapters.ClassAck2xx, "200:REPLAY", "cmp_1", false},
		{"the same key with another case", 409, nil, errBody("IDEMPOTENCY_CONFLICT"), adapters.ClassRejected4xx, "409:IDEMPOTENCY_CONFLICT", "", true},
		{"no such case", 404, nil, errBody("NOT_FOUND"), adapters.ClassRejected4xx, "404:NOT_FOUND", "", false},
		{"server error", 500, nil, errBody("INTERNAL"), adapters.ClassServer5xx, "500:INTERNAL", "", false},
		{"unreadable 2xx", 201, nil, map[string]any{"surprise": 1}, adapters.ClassNoResponse, "201:UNRECOGNISED", "", false},
		{"a 2xx for another case", 201, nil, complaint(map[string]any{"case_id": "cas_other"}), adapters.ClassNoResponse, "201:UNRECOGNISED", "", false},
		{"a 2xx for another tenant", 201, nil, complaint(map[string]any{"tenant_id": "ten_b"}), adapters.ClassNoResponse, "201:UNRECOGNISED", "", false},
		{"a 2xx for another key", 201, nil, complaint(map[string]any{"idempotency_key": "other"}), adapters.ClassNoResponse, "201:UNRECOGNISED", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			var s seen
			r := serveOnce(t, &s, c.status, c.hdr, c.body).LogComplaint(deadline(t), adapters.LogComplaintInput{IdempotencyKey: "idem-cm-1", TenantID: "ten_a", CaseID: "cas_1", Timeout: time.Second})
			if r.Class != c.class || r.RawCode != c.raw || r.ExternalAck != c.ack || r.KeyCollision != c.coll {
				t.Fatalf("got class=%s raw=%s ack=%q collision=%v", r.Class, r.RawCode, r.ExternalAck, r.KeyCollision)
			}
		})
	}
}

// REQ-EXE-007, EVD-05: the read-backs return the record with the hash of the bytes received, refuse unknown members as MALFORMED, and map a 404 to
// NOT_FOUND, not to an empty record.
func TestRecordReadBacks(t *testing.T) {
	var s seen
	ev, err := serveOnce(t, &s, 200, nil, attachment(nil)).GetEvidence(deadline(t), "ten_a", "att_1")
	if err != nil || ev.AttachmentID != "att_1" || ev.Payload != payload || s.path != "/v1/evidence/att_1" || s.method != http.MethodGet {
		t.Fatalf("evidence %+v err %v path %s", ev, err, s.path)
	}
	cm, err := serveOnce(t, &s, 200, nil, complaint(nil)).GetComplaint(deadline(t), "ten_a", "cmp_1")
	if err != nil || cm.ComplaintRef != "cmp_1" || s.path != "/v1/complaints/cmp_1" {
		t.Fatalf("complaint %+v err %v path %s", cm, err, s.path)
	}
	for name, f := range map[string]func(c *CaseClient) error{
		"evidence 404":      func(c *CaseClient) error { _, e := c.GetEvidence(deadline(t), "ten_a", "att_x"); return e },
		"complaint 404":     func(c *CaseClient) error { _, e := c.GetComplaint(deadline(t), "ten_a", "cmp_x"); return e },
		"evidence drift":    func(c *CaseClient) error { _, e := c.GetEvidence(deadline(t), "ten_a", "att_1"); return e },
		"complaint drift":   func(c *CaseClient) error { _, e := c.GetComplaint(deadline(t), "ten_a", "cmp_1"); return e },
		"evidence unavail":  func(c *CaseClient) error { _, e := c.GetEvidence(deadline(t), "ten_a", "att_1"); return e },
		"complaint unavail": func(c *CaseClient) error { _, e := c.GetComplaint(deadline(t), "ten_a", "cmp_1"); return e },
	} {
		status, body, want := 404, any(errBody("NOT_FOUND")), adapters.ReadNotFound
		switch {
		case strings.HasSuffix(name, "drift"):
			status, body, want = 200, map[string]any{"surprise": true}, adapters.ReadMalformed
		case strings.HasSuffix(name, "unavail"):
			status, body, want = 503, errBody("UNAVAILABLE"), adapters.ReadUnavailable
		}
		err := f(serveOnce(t, &s, status, nil, body))
		re, ok := err.(*adapters.ReadError)
		if !ok || re.Failure != want || re.RawHash == "" {
			t.Errorf("%s: %v (want %s with the hash of the bytes received)", name, err, want)
		}
	}
}

// REQ-EXE-007: a find is scoped to a tenant and either an idempotency key or a case; an empty complete page means "none", an incomplete page does
// not; a query that names neither is refused before any request.
func TestRecordFindsAreScopedAndCarryCompleteness(t *testing.T) {
	var s seen
	page := map[string]any{"items": []any{attachment(nil)}, "complete": true, "next_cursor": ""}
	c := serveOnce(t, &s, 200, nil, page)
	got, err := c.FindEvidence(deadline(t), adapters.RecordQuery{TenantID: "ten_a", IdempotencyKey: "idem-ev-1"})
	if err != nil || len(got.Items) != 1 || !got.Complete || s.path != "/v1/evidence" {
		t.Fatalf("page %+v err %v path %s", got, err, s.path)
	}
	if _, err := c.FindComplaints(deadline(t), adapters.RecordQuery{TenantID: "ten_a", CaseID: "cas_1"}); err == nil || s.path != "/v1/complaints" {
		// the canned body is an evidence page, so the complaint find must refuse it as malformed
		t.Fatalf("a complaint find accepted an evidence page: %v", err)
	}
	before := s.hits
	if _, err := c.FindEvidence(deadline(t), adapters.RecordQuery{TenantID: "ten_a"}); err == nil || s.hits != before {
		t.Fatalf("a find with neither key nor case sent a request or succeeded: %v", err)
	}
	if _, err := c.FindEvidence(deadline(t), adapters.RecordQuery{IdempotencyKey: "k"}); err == nil || s.hits != before {
		t.Fatalf("a find with no tenant sent a request or succeeded: %v", err)
	}
	incomplete := serveOnce(t, &s, 200, nil, map[string]any{"items": []any{}, "complete": false, "next_cursor": "c1"})
	pg, err := incomplete.FindEvidence(deadline(t), adapters.RecordQuery{TenantID: "ten_a", CaseID: "cas_1"})
	if err != nil || pg.Complete || pg.NextCursor != "c1" {
		t.Fatalf("incomplete page %+v err %v", pg, err)
	}
}

func asReadFailure(err error) adapters.ReadFailure {
	if re, ok := err.(*adapters.ReadError); ok {
		return re.Failure
	}
	return ""
}

// EVD-05, REQ-ENV-004: a read that is not exactly the record asked for is MALFORMED, never a record: another tenant's, another ID, a payload that
// does not match its hash.
func TestARecordReadThatIsNotTheRecordAskedForIsMalformed(t *testing.T) {
	var s seen
	for name, body := range map[string]map[string]any{
		"another tenant's attachment":    attachment(map[string]any{"tenant_id": "ten_b"}),
		"another attachment id":          attachment(map[string]any{"attachment_id": "att_other"}),
		"a payload that is not its hash": attachment(map[string]any{"payload": "tampered"}),
		"a hash that is not a hash":      attachment(map[string]any{"payload_hash": "md5:abc"}),
	} {
		if _, err := serveOnce(t, &s, 200, nil, body).GetEvidence(deadline(t), "ten_a", "att_1"); asReadFailure(err) != adapters.ReadMalformed {
			t.Errorf("evidence, %s: %v", name, err)
		}
	}
	for name, body := range map[string]map[string]any{
		"another tenant's complaint": complaint(map[string]any{"tenant_id": "ten_b"}),
		"another complaint ref":      complaint(map[string]any{"complaint_ref": "cmp_other"}),
		"no clock":                   complaint(map[string]any{"clock_started_at": ""}),
	} {
		if _, err := serveOnce(t, &s, 200, nil, body).GetComplaint(deadline(t), "ten_a", "cmp_1"); asReadFailure(err) != adapters.ReadMalformed {
			t.Errorf("complaint, %s: %v", name, err)
		}
	}
}

// REQ-EXE-007: a page is complete with no cursor or incomplete with one; any other combination, and a missing marker, is MALFORMED, so an
// incomplete page can never be read as "none".
func TestAPageWithAnInconsistentCompletenessMarkerIsMalformed(t *testing.T) {
	var s seen
	for name, page := range map[string]map[string]any{
		"complete with a cursor":      {"items": []any{}, "complete": true, "next_cursor": "c1"},
		"incomplete with no cursor":   {"items": []any{}, "complete": false, "next_cursor": ""},
		"no completeness marker":      {"items": []any{}, "next_cursor": ""},
		"an item of another tenant":   {"items": []any{attachment(map[string]any{"tenant_id": "ten_b"})}, "complete": true, "next_cursor": ""},
		"an item whose hash is wrong": {"items": []any{attachment(map[string]any{"payload": "tampered"})}, "complete": true, "next_cursor": ""},
	} {
		if _, err := serveOnce(t, &s, 200, nil, page).FindEvidence(deadline(t), adapters.RecordQuery{TenantID: "ten_a", CaseID: "cas_1"}); asReadFailure(err) != adapters.ReadMalformed {
			t.Errorf("evidence page, %s: %v", name, err)
		}
	}
	cpage := func(items ...any) map[string]any {
		return map[string]any{"items": items, "complete": true, "next_cursor": ""}
	}
	if _, err := serveOnce(t, &s, 200, nil, cpage(complaint(map[string]any{"tenant_id": "ten_b"}))).FindComplaints(deadline(t), adapters.RecordQuery{TenantID: "ten_a", CaseID: "cas_1"}); asReadFailure(err) != adapters.ReadMalformed {
		t.Errorf("complaint page with another tenant's item: %v", err)
	}
	if _, err := serveOnce(t, &s, 200, nil, map[string]any{"items": []any{}, "complete": true, "next_cursor": "c"}).FindComplaints(deadline(t), adapters.RecordQuery{TenantID: "ten_a", CaseID: "cas_1"}); asReadFailure(err) != adapters.ReadMalformed {
		t.Errorf("complaint page, complete with a cursor: %v", err)
	}
}
