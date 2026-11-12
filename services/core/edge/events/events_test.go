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

// baseOptions are valid options; a test changes the one it is about.
func baseOptions(feed events.Feed, wait func(context.Context, time.Duration) error) events.Options {
	return events.Options{Feed: feed, Mask: digitsMasker, Wait: wait, Clock: clock.NewManual(epoch), Poll: time.Second, MaxStream: time.Minute, MaxEventBytes: 2048,
		WriteTimeout: 5 * time.Second, MaxStreams: 8, MaxStreamsPerPrincipal: 2, MaxBatch: 32}
}

func newRig(t *testing.T, feed *fakeFeed, wait func(context.Context, time.Duration) error) rig {
	t.Helper()
	return newRigWith(t, feed, wait, nil)
}

func newRigWith(t *testing.T, feed *fakeFeed, wait func(context.Context, time.Duration) error, tweak func(*events.Options)) rig {
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
	o := baseOptions(feed, wait)
	if tweak != nil {
		tweak(&o)
	}
	h, err := events.New(o)
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
	e.Refs = map[string]string{"investigation_id": "acct_0123456789"}
	bad["a ref whose prefix is not its key's ID type (a long digit run behind a typed-looking prefix)"] = e
	e = ev(1, events.KindProgress)
	e.Refs = map[string]string{"account_id": "acct_0123456789"}
	bad["a ref under a key that names no contract ID"] = e
	e = ev(1, events.KindProgress)
	e.Refs = map[string]string{"decision_id": "dec_0123456789_0123456789"}
	bad["a ref with a separator inside its body"] = e
	e = ev(1, events.KindProgress)
	e.Refs = map[string]string{"decision_id": "dec_" + strings.Repeat("A", 33)}
	bad["a ref over the contract's 32 characters"] = e
	e = ev(1, events.KindProgress)
	e.Fields = map[string]string{"text": "x\nid: 9\nevent: answer"}
	bad["a field that could forge a frame"] = e
	e = ev(1, events.KindProgress)
	e.Fields = map[string]string{"text": strings.Repeat("a", 4096)}
	bad["an event over the byte bound"] = e
	e = ev(1, events.KindProgress)
	e.Fields = map[string]string{}
	for i := 0; i < 17; i++ {
		e.Fields["k"+strings.Repeat("a", i)] = "x"
	}
	bad["more members than the bound"] = e
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
	first, ok := f.ctxs[0].Deadline()
	if !ok {
		t.Fatal("the feed context has no stream deadline")
	}
	for i, c := range f.ctxs {
		if d, ok := c.Deadline(); !ok || !d.Equal(first) {
			t.Errorf("poll %d: the feed context's deadline is %v (%v), want the one stream deadline %v", i, d, ok, first)
		}
	}
}

func TestNewRefusesWhatItCannotRunWith(t *testing.T) {
	ok := baseOptions(&fakeFeed{}, func(context.Context, time.Duration) error { return nil })
	if _, err := events.New(ok); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*events.Options){
		"no feed": func(o *events.Options) { o.Feed = nil }, "no masker": func(o *events.Options) { o.Mask = nil }, "no wait": func(o *events.Options) { o.Wait = nil },
		"no poll": func(o *events.Options) { o.Poll = 0 }, "a negative poll": func(o *events.Options) { o.Poll = -time.Second },
		"no stream limit": func(o *events.Options) { o.MaxStream = 0 }, "no event bound": func(o *events.Options) { o.MaxEventBytes = 0 },
		"no clock": func(o *events.Options) { o.Clock = nil }, "no write deadline": func(o *events.Options) { o.WriteTimeout = 0 }, "no global stream cap": func(o *events.Options) { o.MaxStreams = 0 },
		"no per-principal cap": func(o *events.Options) { o.MaxStreamsPerPrincipal = 0 }, "no batch cap": func(o *events.Options) { o.MaxBatch = 0 },
		"a per-principal cap above the global one": func(o *events.Options) { o.MaxStreamsPerPrincipal = o.MaxStreams + 1 },
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
	h, _ := events.New(baseOptions(f, func(context.Context, time.Duration) error { return nil }))
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

// ---- the reviewer's findings on 2229520f (REQ-SEC-003, INV-12, REQ-API-002) ----

// failWriter is a response writer whose Write fails after n successful writes, and which records the write deadlines the handler sets (http.ResponseController).
type failWriter struct {
	*httptest.ResponseRecorder
	okWrites  int
	writes    int
	deadlines []time.Time
}

func (f *failWriter) Write(b []byte) (int, error) {
	f.writes++
	if f.writes > f.okWrites {
		return 0, errors.New("synthetic: the client is gone or too slow")
	}
	return f.ResponseRecorder.Write(b)
}
func (f *failWriter) SetWriteDeadline(t time.Time) error {
	f.deadlines = append(f.deadlines, t)
	return nil
}

func (r rig) request(w http.ResponseWriter, role session.Role) {
	c := edgetest.Good(epoch, role)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+r.iss.Token(edgetest.Kid, c))
	r.h.ServeHTTP(w, req)
}

// A closed or expired session ends the stream: it lasts no longer than the token does, even when MaxStream is longer.
func TestAStreamEndsWhenTheSessionsTokenExpires(t *testing.T) {
	f := &fakeFeed{batches: []events.Batch{{Events: []events.Event{ev(1, events.KindProgress)}}}}
	wait := func(ctx context.Context, _ time.Duration) error { <-ctx.Done(); return ctx.Err() }
	// the handler's clock is 100 ms before the token's expiry (epoch + 5 min); the stream limit is an hour
	r := newRigWith(t, f, wait, func(o *events.Options) {
		o.Clock = clock.NewManual(epoch.Add(5*time.Minute - 100*time.Millisecond))
		o.MaxStream = time.Hour
	})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { rec := httptest.NewRecorder(); r.request(rec, session.RoleCustomer); done <- rec }()
	select {
	case rec := <-done:
		if !strings.Contains(rec.Body.String(), "id: 1\n") || rec.Code != http.StatusOK {
			t.Errorf("%d %q", rec.Code, rec.Body.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the stream outlived the token")
	}
}

// A token that is already at or past its expiry by the handler's clock is refused before anything streams.
func TestATokenAlreadyExpiredByTheHandlersClockGetsNoStream(t *testing.T) {
	f := &fakeFeed{}
	r := newRigWith(t, f, nil, func(o *events.Options) { o.Clock = clock.NewManual(epoch.Add(5 * time.Minute)) })
	rec := httptest.NewRecorder()
	r.request(rec, session.RoleCustomer)
	if rec.Code != http.StatusUnauthorized || len(f.calls) != 0 {
		t.Fatalf("%d %s, calls %+v", rec.Code, rec.Body.String(), f.calls)
	}
}

// A write that fails ends the stream at once (a slow or gone consumer), and every write has a deadline from the injected clock.
func TestAFailedWriteEndsTheStreamAndEveryWriteHasADeadline(t *testing.T) {
	f := &fakeFeed{batches: []events.Batch{{Events: []events.Event{ev(1, events.KindProgress), ev(2, events.KindProgress)}}, {Events: []events.Event{ev(3, events.KindProgress)}}}}
	r := newRig(t, f, nil)
	w := &failWriter{ResponseRecorder: httptest.NewRecorder(), okWrites: 1}
	r.request(w, session.RoleCustomer)
	if w.writes != 2 {
		t.Errorf("%d writes attempted; want 2 (the second failed and ended the stream)", w.writes)
	}
	if len(f.calls) != 1 {
		t.Errorf("the feed was asked %d times after a failed write; want 1", len(f.calls))
	}
	if len(w.deadlines) == 0 {
		t.Fatal("no write deadline was set")
	}
	for _, d := range w.deadlines {
		if !d.Equal(epoch.Add(5 * time.Second)) {
			t.Errorf("write deadline %v; want the clock's now plus the write timeout (%v)", d, epoch.Add(5*time.Second))
		}
	}
}

// A batch larger than the cap is a bad batch: the stream ends with the safe fault and none of it is written.
func TestABatchOverTheCapEndsTheStreamAndNothingOfItIsWritten(t *testing.T) {
	var evs []events.Event
	for i := int64(1); i <= 4; i++ {
		evs = append(evs, ev(i, events.KindProgress))
	}
	f := &fakeFeed{batches: []events.Batch{{Events: evs}}}
	r := newRigWith(t, f, nil, func(o *events.Options) { o.MaxBatch = 3 })
	rec := httptest.NewRecorder()
	r.request(rec, session.RoleCustomer)
	if strings.Contains(rec.Body.String(), "id: ") || !strings.Contains(rec.Body.String(), "STREAM_FAULT") {
		t.Errorf("%q", rec.Body.String())
	}
}

// Streams are bounded per principal and overall; a refused stream is the retryable 503, and a finished stream gives its place back.
func TestStreamsAreBoundedPerPrincipalAndOverallAndAFinishedStreamFreesItsPlace(t *testing.T) {
	hold := make(chan struct{})
	started := make(chan struct{}, 8)
	wait := func(ctx context.Context, _ time.Duration) error {
		started <- struct{}{}
		select {
		case <-hold:
			return errors.New("released")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f := &fakeFeed{batches: []events.Batch{{}, {}, {}, {}, {}, {}}}
	r := newRigWith(t, f, wait, func(o *events.Options) { o.MaxStreams = 2; o.MaxStreamsPerPrincipal = 1 })
	var wg sync.WaitGroup
	open := func() {
		wg.Add(1)
		go func() { defer wg.Done(); r.request(httptest.NewRecorder(), session.RoleCustomer) }()
		<-started
	}
	open() // prn_1 holds its one place
	rec := httptest.NewRecorder()
	r.request(rec, session.RoleCustomer)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "DEPENDENCY_UNAVAILABLE") {
		t.Errorf("a second stream of the same principal: %d %s", rec.Code, rec.Body.String())
	}
	close(hold)
	wg.Wait()
	rec = httptest.NewRecorder()
	r.request(rec, session.RoleCustomer)
	if rec.Code != http.StatusOK {
		t.Errorf("after the first stream ended: %d %s", rec.Code, rec.Body.String())
	}
}

// The handler itself refuses a session that is not a customer's, the same 404, whatever the mount does (defence in depth for the role gate).
func TestTheHandlerItselfRefusesANonCustomerWithTheSame404(t *testing.T) {
	f := &fakeFeed{}
	o := baseOptions(f, func(context.Context, time.Duration) error { return nil })
	h, err := events.New(o)
	if err != nil {
		t.Fatal(err)
	}
	iss := edgetest.NewIssuer(t)
	clk := clock.NewManual(epoch)
	v, _ := session.NewVerifier(session.Config{Keys: mapKey(iss), Audience: edgetest.Audience, Tenants: []string{edgetest.Tenant}, MaxLifetime: session.CeilingLifetime, Skew: time.Minute, Clock: clk})
	mux := http.NewServeMux()
	mux.Handle(events.Route, session.Middleware(v, ids.New(clk, nil))(h)) // no RequireRole
	for _, role := range []session.Role{session.RoleOps, session.RoleAudit, session.RoleApprover} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+iss.Token(edgetest.Kid, edgetest.Good(epoch, role)))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "NOT_FOUND_OR_NOT_AUTHORIZED") {
			t.Errorf("%s: %d %s", role, rec.Code, rec.Body.String())
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("a non-customer reached the feed: %+v", f.calls)
	}
}

func TestTheStreamTellsProxiesNotToBuffer(t *testing.T) {
	r := newRig(t, &fakeFeed{batches: []events.Batch{{Events: []events.Event{ev(1, events.KindAnswer)}, Done: true}}}, nil)
	rec := httptest.NewRecorder()
	r.request(rec, session.RoleCustomer)
	if got := rec.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering = %q, want no", got)
	}
}

// The global cap holds across principals: one stream open, a second principal is refused.
func TestTheGlobalStreamCapHoldsAcrossPrincipals(t *testing.T) {
	hold := make(chan struct{})
	started := make(chan struct{}, 4)
	wait := func(ctx context.Context, _ time.Duration) error {
		started <- struct{}{}
		select {
		case <-hold:
			return errors.New("released")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f := &fakeFeed{batches: []events.Batch{{}, {}, {}}}
	r := newRigWith(t, f, wait, func(o *events.Options) { o.MaxStreams = 1; o.MaxStreamsPerPrincipal = 1 })
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); r.request(httptest.NewRecorder(), session.RoleCustomer) }()
	<-started
	c := edgetest.Good(epoch, session.RoleCustomer)
	c.Sub = "prn_2"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+r.iss.Token(edgetest.Kid, c))
	rec := httptest.NewRecorder()
	r.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("a second principal over the global cap: %d %s", rec.Code, rec.Body.String())
	}
	close(hold)
	wg.Wait()
}
