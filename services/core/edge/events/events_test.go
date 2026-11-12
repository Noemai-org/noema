package events_test

// EDG-02 (the SSE events, ruling A170; REQ-API-001, PERD 39.2 "GET /v1/conversations/{conversation_ref}/events"; REQ-API-002, REQ-SEC-003, INV-12, AGENT-13).
// The route streams progress, clarification, confirmation, answer and error events of one conversation, from COMMITTED state only (PERD 40: "SSE events are
// emitted from committed state only"): the handler writes nothing the Feed has not returned. The principal and tenant are the verified session's; a conversation that is
// not the caller's is the same 404 as one that does not exist; a free-text field is masked, a typed ID is validated and never masked, a bad event ends the stream with an
// error event that names no internals; Last-Event-ID resumes; the stream ends when the conversation is done, when the client leaves, or at its configured limit. Keys and
// tokens are SYNTHETIC, made per test (AGENT-25).

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/edge/edgetest"
	"github.com/Noemai-org/noema/services/core/edge/events"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
)

var epoch = time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)

type call struct {
	p     session.Principal
	ref   string
	after int64
}

type fakeFeed struct {
	mu      sync.Mutex
	batches []events.Batch
	errs    []error
	calls   []call
	ctxs    []context.Context
}

func (f *fakeFeed) Since(ctx context.Context, p session.Principal, ref string, after int64) (events.Batch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := len(f.calls)
	f.calls = append(f.calls, call{p, ref, after})
	f.ctxs = append(f.ctxs, ctx)
	if n < len(f.errs) && f.errs[n] != nil {
		return events.Batch{}, f.errs[n]
	}
	if n < len(f.batches) {
		return f.batches[n], nil
	}
	return events.Batch{Done: true}, nil
}

// digitsMasker is a test masker: a run of more than 4 digits keeps its last four.
func digitsMasker(s string) string {
	return regexp.MustCompile(`[0-9]{5,}`).ReplaceAllStringFunc(s, func(r string) string { return strings.Repeat("*", len(r)-4) + r[len(r)-4:] })
}

type rig struct {
	iss   *edgetest.Issuer
	feed  *fakeFeed
	h     http.Handler
	waits *int
}

func newRig(t *testing.T, feed *fakeFeed, wait func(context.Context, time.Duration) error) rig {
	t.Helper()
	iss := edgetest.NewIssuer(t)
	clk := clock.NewManual(epoch)
	idg := ids.New(clk, nil)
	v, err := session.NewVerifier(session.Config{Keys: mapKey(iss), Audience: edgetest.Audience, Tenants: []string{edgetest.Tenant, "tnt_other"},
		MaxLifetime: session.CeilingLifetime, Skew: time.Minute, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	waits := new(int)
	if wait == nil {
		wait = func(context.Context, time.Duration) error { *waits++; return nil }
	}
	h, err := events.New(events.Options{Feed: feed, Mask: digitsMasker, Wait: wait, Poll: time.Second, MaxStream: time.Minute, MaxEventBytes: 2048})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle(events.Route, session.Middleware(v, idg)(session.RequireRole(idg, session.RoleCustomer)(h)))
	return rig{iss: iss, feed: feed, h: mux, waits: waits}
}

func (r rig) get(path, tenant string, role session.Role, hdr map[string]string) *httptest.ResponseRecorder {
	c := edgetest.Good(epoch, role)
	c.Tenant = tenant
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+r.iss.Token(edgetest.Kid, c))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	r.h.ServeHTTP(rec, req)
	return rec
}

func ev(cursor int64, kind events.Kind) events.Event {
	return events.Event{Cursor: cursor, Kind: kind, At: epoch.Add(time.Duration(cursor) * time.Second), Refs: map[string]string{"investigation_id": "inv_AA11"},
		Fields: map[string]string{"state": "INTERPRETING"}, Numbers: map[string]int64{"version": cursor}}
}

const path = "/v1/conversations/inv_AA11/events"

func TestAStreamIsServerSentEventsFromTheFeedAndEndsWhenTheConversationIsDone(t *testing.T) {
	f := &fakeFeed{batches: []events.Batch{{Events: []events.Event{ev(1, events.KindProgress), ev(2, events.KindProgress)}}, {Events: []events.Event{ev(3, events.KindAnswer)}, Done: true}}}
	r := newRig(t, f, nil)
	rec := r.get(path, edgetest.Tenant, session.RoleCustomer, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/event-stream" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	want := "id: 1\nevent: progress\ndata: {\"at\":\"2026-11-12T09:00:01Z\",\"fields\":{\"state\":\"INTERPRETING\"},\"numbers\":{\"version\":1},\"refs\":{\"investigation_id\":\"inv_AA11\"}}\n\n" +
		"id: 2\nevent: progress\ndata: {\"at\":\"2026-11-12T09:00:02Z\",\"fields\":{\"state\":\"INTERPRETING\"},\"numbers\":{\"version\":2},\"refs\":{\"investigation_id\":\"inv_AA11\"}}\n\n" +
		"id: 3\nevent: answer\ndata: {\"at\":\"2026-11-12T09:00:03Z\",\"fields\":{\"state\":\"INTERPRETING\"},\"numbers\":{\"version\":3},\"refs\":{\"investigation_id\":\"inv_AA11\"}}\n\n"
	if rec.Body.String() != want {
		t.Fatalf("body:\n%q\nwant:\n%q", rec.Body.String(), want)
	}
	if len(f.calls) != 2 || f.calls[1].after != 2 || *r.waits != 1 {
		t.Fatalf("calls %+v, waits %d; want a second poll after cursor 2 and one wait between", f.calls, *r.waits)
	}
}

// INV-12: the feed is asked as the token's principal and tenant; nothing the request says about either is used.
func TestTheFeedIsAskedWithTheSessionsPrincipalAndTenantNeverTheRequests(t *testing.T) {
	f := &fakeFeed{}
	r := newRig(t, f, nil)
	rec := r.get(path+"?tenant_id=tnt_x&principal_id=prn_x", "tnt_other", session.RoleCustomer, map[string]string{"Noema-Tenant-Id": "tnt_x", "X-Principal": "prn_x"})
	if rec.Code != http.StatusOK || len(f.calls) != 1 {
		t.Fatalf("%d, calls %+v", rec.Code, f.calls)
	}
	if c := f.calls[0]; c.p.Tenant != "tnt_other" || c.p.ID != "prn_1" || c.ref != "inv_AA11" || c.after != 0 {
		t.Fatalf("feed asked %+v", c)
	}
}

func TestLastEventIdResumesAfterThatCursorAndAMalformedOneIsRefused(t *testing.T) {
	f := &fakeFeed{}
	r := newRig(t, f, nil)
	if rec := r.get(path, edgetest.Tenant, session.RoleCustomer, map[string]string{"Last-Event-ID": "41"}); rec.Code != http.StatusOK || f.calls[0].after != 41 {
		t.Fatalf("%d, calls %+v", rec.Code, f.calls)
	}
	n := len(f.calls)
	for _, bad := range []string{"abc", "-1", "4 1", "1.5", "99999999999999999999", "0x10", "+5"} {
		rec := r.get(path, edgetest.Tenant, session.RoleCustomer, map[string]string{"Last-Event-ID": bad})
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "INPUT_INVALID") {
			t.Errorf("Last-Event-ID %q: %d %s", bad, rec.Code, rec.Body.String())
		}
	}
	if len(f.calls) != n {
		t.Errorf("the feed was asked for a refused request")
	}
}

// REQ-API-002: a conversation that is not the caller's, one that does not exist and one that is not an ID are the same answer.
func TestAConversationThatIsNotTheCallersLooksLikeOneThatDoesNotExist(t *testing.T) {
	f := &fakeFeed{errs: []error{events.ErrNotFound, events.ErrNotFound}}
	r := newRig(t, f, nil)
	a := r.get(path, edgetest.Tenant, session.RoleCustomer, nil)
	b := r.get("/v1/conversations/inv_ZZ99/events", edgetest.Tenant, session.RoleCustomer, nil)
	c := r.get("/v1/conversations/not-an-id/events", edgetest.Tenant, session.RoleCustomer, nil)
	for name, rec := range map[string]*httptest.ResponseRecorder{"foreign": a, "missing": b, "malformed": c} {
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "NOT_FOUND_OR_NOT_AUTHORIZED") {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	if len(f.calls) != 2 {
		t.Errorf("a malformed conversation reference reached the feed: %+v", f.calls)
	}
}

func TestAFeedFailureBeforeTheStreamIs503AndAfterItIsASafeErrorEvent(t *testing.T) {
	f := &fakeFeed{errs: []error{errors.New("pq: password authentication failed for user secret_user")}}
	r := newRig(t, f, nil)
	rec := r.get(path, edgetest.Tenant, session.RoleCustomer, nil)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "DEPENDENCY_UNAVAILABLE") || strings.Contains(rec.Body.String(), "secret_user") {
		t.Fatalf("before the stream: %d %s", rec.Code, rec.Body.String())
	}
	f2 := &fakeFeed{batches: []events.Batch{{Events: []events.Event{ev(1, events.KindProgress)}}}, errs: []error{nil, errors.New("connection to 10.0.0.7 refused")}}
	rec = newRig(t, f2, nil).get(path, edgetest.Tenant, session.RoleCustomer, nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.HasPrefix(body, "id: 1\nevent: progress\n") || !strings.HasSuffix(body, "event: error\ndata: {\"code\":\"STREAM_UNAVAILABLE\"}\n\n") || strings.Contains(body, "10.0.0.7") {
		t.Fatalf("after the stream began: %d %q", rec.Code, body)
	}
}

// AGENT-13, A154: free text is masked; typed IDs are validated and never masked; numbers and instants are typed.
func TestFreeTextIsMaskedAndTypedIdsAreValidatedAndNeverMasked(t *testing.T) {
	e := ev(1, events.KindAnswer)
	e.Fields = map[string]string{"text": "Account 0123456789 is pending."}
	e.Refs = map[string]string{"decision_id": "dec_0123456789ABCDEF"}
	f := &fakeFeed{batches: []events.Batch{{Events: []events.Event{e}, Done: true}}}
	body := newRig(t, f, nil).get(path, edgetest.Tenant, session.RoleCustomer, nil).Body.String()
	if !strings.Contains(body, `"text":"Account ******6789 is pending."`) || !strings.Contains(body, `"decision_id":"dec_0123456789ABCDEF"`) || strings.Contains(body, "0123456789 ") {
		t.Fatalf("body %q", body)
	}
}

// A bad event from the feed ends the stream with an error event and nothing of the bad event is written. One case per rule.
func TestAnEventTheFeedGotWrongEndsTheStreamWithAnErrorEventAndIsNeverWritten(t *testing.T) {
	bad := map[string]events.Event{}
	e := ev(1, events.KindProgress)
	e.Kind = "debug"
	bad["an unknown kind"] = e
	e = ev(1, events.KindProgress)
	e.Cursor = 0
	bad["a cursor of zero"] = e
	e = ev(1, events.KindProgress)
	e.At = time.Time{}
	bad["no instant"] = e
	e = ev(1, events.KindProgress)
	e.Refs = map[string]string{"investigation_id": "0123456789012"}
	bad["a ref that is not a typed ID"] = e
	e = ev(1, events.KindProgress)
	e.Refs = map[string]string{"Investigation": "inv_AA11"}
	bad["a key outside its pattern"] = e
	e = ev(1, events.KindProgress)
	e.Fields = map[string]string{"text": "x\nid: 9\nevent: answer"}
	bad["a field that could forge a frame"] = e
	e = ev(1, events.KindProgress)
	e.Fields = map[string]string{"text": strings.Repeat("a", 4096)}
	bad["an event over the byte bound"] = e
	for name, b := range bad {
		f := &fakeFeed{batches: []events.Batch{{Events: []events.Event{b}, Done: true}}}
		body := newRig(t, f, nil).get(path, edgetest.Tenant, session.RoleCustomer, nil).Body.String()
		if body != "event: error\ndata: {\"code\":\"STREAM_FAULT\"}\n\n" {
			t.Errorf("%s: %q", name, body)
		}
	}
	f := &fakeFeed{batches: []events.Batch{{Events: []events.Event{ev(3, events.KindProgress), ev(3, events.KindProgress)}, Done: true}}}
	body := newRig(t, f, nil).get(path, edgetest.Tenant, session.RoleCustomer, nil).Body.String()
	if !strings.HasPrefix(body, "id: 3\n") || strings.Count(body, "id: 3\n") != 1 || !strings.HasSuffix(body, "event: error\ndata: {\"code\":\"STREAM_FAULT\"}\n\n") {
		t.Errorf("a repeated cursor: %q", body)
	}
	f = &fakeFeed{batches: []events.Batch{{Events: []events.Event{ev(5, events.KindProgress)}, Done: true}}}
	if body := newRig(t, f, nil).get(path, edgetest.Tenant, session.RoleCustomer, map[string]string{"Last-Event-ID": "5"}).Body.String(); !strings.HasPrefix(body, "event: error") {
		t.Errorf("a cursor not above the one asked after: %q", body)
	}
}

// The stream is bounded: it polls with the configured wait, sends a comment as a keepalive when nothing is new, stops when the client leaves, and its feed context
// carries the stream's own deadline.
func TestTheStreamKeepsAliveStopsWhenTheClientLeavesAndCarriesItsDeadline(t *testing.T) {
	f := &fakeFeed{batches: []events.Batch{{}, {}, {Events: []events.Event{ev(1, events.KindProgress)}}}}
	ctx, cancel := context.WithCancel(context.Background())
	waits := 0
	wait := func(c context.Context, _ time.Duration) error {
		waits++
		if waits == 3 {
			cancel()
		}
		return c.Err()
	}
	r := newRig(t, f, wait)
	req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
	c := edgetest.Good(epoch, session.RoleCustomer)
	req.Header.Set("Authorization", "Bearer "+r.iss.Token(edgetest.Kid, c))
	rec := httptest.NewRecorder()
	r.h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if strings.Count(body, ": keepalive\n\n") != 2 || !strings.Contains(body, "id: 1\nevent: progress\n") || waits != 3 {
		t.Fatalf("waits %d, body %q", waits, body)
	}
	for i, c := range f.ctxs {
		d, ok := c.Deadline()
		if !ok || d.Sub(time.Now()) > time.Minute+time.Second {
			t.Errorf("poll %d: the feed context has no stream deadline within a minute (%v, %v)", i, d, ok)
		}
	}
}

func TestNewRefusesWhatItCannotRunWith(t *testing.T) {
	ok := events.Options{Feed: &fakeFeed{}, Mask: digitsMasker, Wait: func(context.Context, time.Duration) error { return nil }, Poll: time.Second, MaxStream: time.Minute, MaxEventBytes: 1024}
	if _, err := events.New(ok); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*events.Options){
		"no feed": func(o *events.Options) { o.Feed = nil }, "no masker": func(o *events.Options) { o.Mask = nil }, "no wait": func(o *events.Options) { o.Wait = nil },
		"no poll": func(o *events.Options) { o.Poll = 0 }, "a negative poll": func(o *events.Options) { o.Poll = -time.Second },
		"no stream limit": func(o *events.Options) { o.MaxStream = 0 }, "no event bound": func(o *events.Options) { o.MaxEventBytes = 0 },
	}
	for name, mut := range cases {
		o := ok
		mut(&o)
		if h, err := events.New(o); err == nil || h != nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Only a customer streams: other roles are refused by the edge gate before the handler (and so before the feed).
func TestOnlyACustomerSessionReachesTheFeed(t *testing.T) {
	f := &fakeFeed{}
	r := newRig(t, f, nil)
	for _, role := range []session.Role{session.RoleOps, session.RoleAudit, session.RoleApprover} {
		if rec := r.get(path, edgetest.Tenant, role, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", role, rec.Code)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("a non-customer reached the feed: %+v", f.calls)
	}
}

type noFlush struct{ http.ResponseWriter }

func TestAWriterThatCannotFlushIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	f := &fakeFeed{}
	r := newRig(t, f, nil)
	h, _ := events.New(events.Options{Feed: f, Mask: digitsMasker, Wait: func(context.Context, time.Duration) error { return nil }, Poll: time.Second, MaxStream: time.Minute, MaxEventBytes: 1024})
	c := edgetest.Good(epoch, session.RoleCustomer)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.SetPathValue("conversation_ref", "inv_AA11")
	req.Header.Set("Authorization", "Bearer "+r.iss.Token(edgetest.Kid, c))
	rec := httptest.NewRecorder()
	// the handler alone, with a principal in the context but a writer that cannot flush
	clk := clock.NewManual(epoch)
	v, _ := session.NewVerifier(session.Config{Keys: mapKey(r.iss), Audience: edgetest.Audience, Tenants: []string{edgetest.Tenant}, MaxLifetime: session.CeilingLifetime, Skew: time.Minute, Clock: clk})
	session.Middleware(v, ids.New(clk, nil))(h).ServeHTTP(noFlush{rec}, req)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "INTERNAL_SAFETY_BLOCK") || len(f.calls) != 0 {
		t.Fatalf("%d %s, calls %+v", rec.Code, rec.Body.String(), f.calls)
	}
	_ = io.Discard
}

func mapKey(iss *edgetest.Issuer) map[string]*ecdsa.PublicKey {
	return map[string]*ecdsa.PublicKey{edgetest.Kid: iss.Public()}
}
