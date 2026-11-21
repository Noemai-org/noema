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
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/edge/edgetest"
	"github.com/Noemai-org/noema/services/core/edge/events"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
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

// deadlineRec is a test response writer that supports write deadlines, as a real server's writer does; the handler refuses a writer that does not.
type deadlineRec struct{ *httptest.ResponseRecorder }

func (deadlineRec) SetWriteDeadline(time.Time) error { return nil }

func newRec() *deadlineRec { return &deadlineRec{httptest.NewRecorder()} }

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

func (r rig) get(path, tenant string, role session.Role, hdr map[string]string) *deadlineRec {
	c := edgetest.Good(epoch, role)
	c.Tenant = tenant
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+r.iss.Token(edgetest.Kid, c))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := newRec()
	r.h.ServeHTTP(rec, req)
	return rec
}

type kind string

const (
	kindProgress kind = "progress"
	kindAnswer   kind = "answer"
)

func stamp(cursor int64) contracts.CommonV1Timestamp {
	return contracts.CommonV1Timestamp(epoch.Add(time.Duration(cursor) * time.Second).Format(time.RFC3339))
}

// ev is one committed event: a progress event, or (kindAnswer) a minimal valid answer.
func ev(cursor int64, k kind) events.Event {
	if k == kindAnswer {
		return events.Event{Cursor: cursor, Answer: &contracts.SseEventV1Answer{At: stamp(cursor), Answer: contracts.CustomerAnswerV1{SchemaVersion: "1.0", Found: []string{"Your card is active."}, NotKnown: []contracts.CustomerAnswerV1NotKnownItem{},
			Done: contracts.CustomerAnswerV1Done{Summary: "Checked."}, Advice: contracts.CustomerAnswerV1Advice{Do: []string{}, DoNot: []string{}}, Next: contracts.CustomerAnswerV1Next{What: "Nothing more."}}}}
	}
	return events.Event{Cursor: cursor, Progress: &contracts.SseEventV1Progress{At: stamp(cursor), State: contracts.InvestigationResponseV1PublicStageWorking}}
}

func clarification(cursor int64, question, id string) events.Event {
	return events.Event{Cursor: cursor, Clarification: &contracts.SseEventV1Clarification{At: stamp(cursor), Card: contracts.OverlayCardV1ClarificationCard{CardType: "CLARIFICATION", SchemaVersion: "1.0",
		ClarificationId: contracts.CommonV1ClarificationId(id), Question: question, Options: []contracts.OverlayCardV1ClarificationCardOptionsItem{{OptionId: "opt1", Label: "Option one"}}, AllowNone: true, ExpiresAt: stamp(cursor + 60)}}}
}

// isFault reports whether body is exactly one safe error event (the sse-event.v1 error payload) with the given code and nothing else written before it.
func isFault(body, code string) bool {
	return strings.HasPrefix(body, "event: error\ndata: {\"event\":\"error\",\"schema_version\":\"1.0\"") && strings.Contains(body, `"code":"`+code+`"`) && strings.Count(body, "event: ") == 1 && !strings.Contains(body, "id: ")
}

const path = "/v1/conversations/inv_AA11/events"

func TestAStreamIsServerSentEventsFromTheFeedAndEndsWhenTheConversationIsDone(t *testing.T) {
	f := &fakeFeed{batches: []events.Batch{{Events: []events.Event{ev(1, kindProgress), ev(2, kindProgress)}}, {Events: []events.Event{ev(3, kindAnswer)}, Done: true}}}
	r := newRig(t, f, nil)
	rec := r.get(path, edgetest.Tenant, session.RoleCustomer, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/event-stream" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	pf := func(c int, at string) string {
		return "id: " + strconv.Itoa(c) + "\nevent: progress\ndata: {\"event\":\"progress\",\"schema_version\":\"1.0\",\"at\":\"" + at + "\",\"state\":\"working\"}\n\n"
	}
	want := pf(1, "2026-11-12T09:00:01Z") + pf(2, "2026-11-12T09:00:02Z") + "id: 3\nevent: answer\ndata: {\"event\":\"answer\",\"schema_version\":\"1.0\",\"at\":\"2026-11-12T09:00:03Z\",\"answer\":" +
		`{"schema_version":"1.0","found":["Your card is active."],"not_known":[],"done":{"summary":"Checked.","verified":false},"advice":{"do":[],"do_not":[]},"next":{"what":"Nothing more."}}}` + "\n\n"
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
	for name, rec := range map[string]*deadlineRec{"foreign": a, "missing": b, "malformed": c} {
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
	f2 := &fakeFeed{batches: []events.Batch{{Events: []events.Event{ev(1, kindProgress)}}}, errs: []error{nil, errors.New("connection to 10.0.0.7 refused")}}
	rec = newRig(t, f2, nil).get(path, edgetest.Tenant, session.RoleCustomer, nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.HasPrefix(body, "id: 1\nevent: progress\n") || !strings.Contains(body, "event: error\ndata: {\"event\":\"error\"") || !strings.Contains(body, `"code":"DEPENDENCY_UNAVAILABLE","message":"The service is not available now.","retryable":true`) || strings.Contains(body, "10.0.0.7") {
		t.Fatalf("after the stream began: %d %q", rec.Code, body)
	}
}

// AGENT-13, A154: the contract's texts carry no run of five or more digits. A text the masker would change is a bad event and is never written; the card's own IDs are checked
// by their contract shape and are exempt (a ULID may hold a long digit run).
func TestATextThatBreaksTheDigitRuleIsABadEventAndACardsIdsAreNotMasked(t *testing.T) {
	bad := &fakeFeed{batches: []events.Batch{{Events: []events.Event{clarification(1, "Is account 0123456789 the one?", "clr_AA11")}, Done: true}}}
	if body := newRig(t, bad, nil).get(path, edgetest.Tenant, session.RoleCustomer, nil).Body.String(); !isFault(body, "INTERNAL_SAFETY_BLOCK") {
		t.Fatalf("an unmasked digit run: %q", body)
	}
	good := &fakeFeed{batches: []events.Batch{{Events: []events.Event{clarification(1, "Which payment do you mean?", "clr_0123456789ABCDEF")}, Done: true}}}
	body := newRig(t, good, nil).get(path, edgetest.Tenant, session.RoleCustomer, nil).Body.String()
	if !strings.HasPrefix(body, "id: 1\nevent: clarification\n") || !strings.Contains(body, `"clarification_id":"clr_0123456789ABCDEF"`) {
		t.Fatalf("%q", body)
	}
}

// A bad event from the feed ends the stream with an error event and nothing of the bad event is written. One case per rule.
func TestAnEventTheFeedGotWrongEndsTheStreamWithAnErrorEventAndIsNeverWritten(t *testing.T) {
	bad := map[string]events.Event{}
	bad["no payload"] = events.Event{Cursor: 1}
	e := ev(1, kindProgress)
	e.Answer = ev(1, kindAnswer).Answer
	bad["two payloads"] = e
	e = ev(1, kindProgress)
	e.Cursor = 0
	bad["a cursor of zero"] = e
	e = ev(1, kindProgress)
	e.Progress.At = ""
	bad["no instant"] = e
	e = ev(1, kindProgress)
	e.Progress.At = "2026-11-12T11:00:00+02:00"
	bad["an instant that is not UTC with a Z"] = e
	e = ev(1, kindProgress)
	e.Progress.State = "debug_internal_state"
	bad["a stage outside the contract"] = e
	e = clarification(1, "Which one?", "clr_AA11")
	e.Clarification.Card.ClarificationId = "acct_0123456789"
	bad["a card ID that is not its contract ID"] = e
	e = clarification(1, strings.Repeat("a", 4096), "clr_AA11")
	bad["an event over the byte bound"] = e
	for name, b := range bad {
		f := &fakeFeed{batches: []events.Batch{{Events: []events.Event{b}, Done: true}}}
		body := newRig(t, f, nil).get(path, edgetest.Tenant, session.RoleCustomer, nil).Body.String()
		if !isFault(body, "INTERNAL_SAFETY_BLOCK") {
			t.Errorf("%s: %q", name, body)
		}
	}
	f := &fakeFeed{batches: []events.Batch{{Events: []events.Event{ev(3, kindProgress), ev(3, kindProgress)}, Done: true}}}
	body := newRig(t, f, nil).get(path, edgetest.Tenant, session.RoleCustomer, nil).Body.String()
	if !strings.HasPrefix(body, "id: 3\n") || strings.Count(body, "id: 3\n") != 1 || !strings.Contains(body, "event: error\n") {
		t.Errorf("a repeated cursor: %q", body)
	}
	f = &fakeFeed{batches: []events.Batch{{Events: []events.Event{ev(5, kindProgress)}, Done: true}}}
	if body := newRig(t, f, nil).get(path, edgetest.Tenant, session.RoleCustomer, map[string]string{"Last-Event-ID": "5"}).Body.String(); !strings.HasPrefix(body, "event: error") {
		t.Errorf("a cursor not above the one asked after: %q", body)
	}
}

// The handler sets the payload's event name from which member is set: a Feed cannot name one kind and send another, and what is written is the contract's payload only.
func TestTheEventNameComesFromThePayloadAndNothingInternalCanBeAdded(t *testing.T) {
	e := ev(1, kindProgress)
	e.Progress.Event = "answer"
	e.Progress.SchemaVersion = "9.9"
	body := newRig(t, &fakeFeed{batches: []events.Batch{{Events: []events.Event{e}, Done: true}}}, nil).get(path, edgetest.Tenant, session.RoleCustomer, nil).Body.String()
	if !strings.HasPrefix(body, "id: 1\nevent: progress\ndata: {\"event\":\"progress\",\"schema_version\":\"1.0\",") {
		t.Fatalf("%q", body)
	}
	var m map[string]any
	line := strings.TrimSuffix(strings.SplitN(body, "data: ", 2)[1], "\n\n")
	if err := json.Unmarshal([]byte(line), &m); err != nil || len(m) != 4 {
		t.Fatalf("members %v, %v; want exactly event, schema_version, at and state", m, err)
	}
}

// The stream is bounded: it polls with the configured wait, sends a comment as a keepalive when nothing is new, stops when the client leaves, and its feed context
// carries the stream's own deadline.
func TestTheStreamKeepsAliveStopsWhenTheClientLeavesAndCarriesItsDeadline(t *testing.T) {
	f := &fakeFeed{batches: []events.Batch{{}, {}, {Events: []events.Event{ev(1, kindProgress)}}}}
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
	rec := newRec()
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
	rec := newRec()
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
	f := &fakeFeed{batches: []events.Batch{{Events: []events.Event{ev(1, kindProgress)}}}}
	wait := func(ctx context.Context, _ time.Duration) error { <-ctx.Done(); return ctx.Err() }
	// the handler's clock is 100 ms before the token's expiry (epoch + 5 min); the stream limit is an hour
	r := newRigWith(t, f, wait, func(o *events.Options) {
		o.Clock = clock.NewManual(epoch.Add(5*time.Minute - 100*time.Millisecond))
		o.MaxStream = time.Hour
	})
	done := make(chan *deadlineRec, 1)
	go func() { rec := newRec(); r.request(rec, session.RoleCustomer); done <- rec }()
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
	rec := newRec()
	r.request(rec, session.RoleCustomer)
	if rec.Code != http.StatusUnauthorized || len(f.calls) != 0 {
		t.Fatalf("%d %s, calls %+v", rec.Code, rec.Body.String(), f.calls)
	}
}

// A write that fails ends the stream at once (a slow or gone consumer), and every write has a deadline from the injected clock.
func TestAFailedWriteEndsTheStreamAndEveryWriteHasADeadline(t *testing.T) {
	f := &fakeFeed{batches: []events.Batch{{Events: []events.Event{ev(1, kindProgress), ev(2, kindProgress)}}, {Events: []events.Event{ev(3, kindProgress)}}}}
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
		evs = append(evs, ev(i, kindProgress))
	}
	f := &fakeFeed{batches: []events.Batch{{Events: evs}}}
	r := newRigWith(t, f, nil, func(o *events.Options) { o.MaxBatch = 3 })
	rec := newRec()
	r.request(rec, session.RoleCustomer)
	if strings.Contains(rec.Body.String(), "id: ") || !strings.Contains(rec.Body.String(), "INTERNAL_SAFETY_BLOCK") {
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
		go func() { defer wg.Done(); r.request(newRec(), session.RoleCustomer) }()
		<-started
	}
	open() // prn_1 holds its one place
	rec := newRec()
	r.request(rec, session.RoleCustomer)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "DEPENDENCY_UNAVAILABLE") {
		t.Errorf("a second stream of the same principal: %d %s", rec.Code, rec.Body.String())
	}
	close(hold)
	wg.Wait()
	rec = newRec()
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
		rec := newRec()
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
	r := newRig(t, &fakeFeed{batches: []events.Batch{{Events: []events.Event{ev(1, kindAnswer)}, Done: true}}}, nil)
	rec := newRec()
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
	go func() { defer wg.Done(); r.request(newRec(), session.RoleCustomer) }()
	<-started
	c := edgetest.Good(epoch, session.RoleCustomer)
	c.Sub = "prn_2"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+r.iss.Token(edgetest.Kid, c))
	rec := newRec()
	r.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("a second principal over the global cap: %d %s", rec.Code, rec.Body.String())
	}
	close(hold)
	wg.Wait()
}

// A writer that cannot take a write deadline gets no stream (fail closed): nothing is written and the feed is not asked.
func TestAWriterThatCannotTakeAWriteDeadlineGetsNoStream(t *testing.T) {
	f := &fakeFeed{}
	r := newRig(t, f, nil)
	rec := httptest.NewRecorder() // a ResponseRecorder has no SetWriteDeadline
	r.request(rec, session.RoleCustomer)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "INTERNAL_SAFETY_BLOCK") || len(f.calls) != 0 {
		t.Fatalf("%d %s, calls %+v", rec.Code, rec.Body.String(), f.calls)
	}
}
