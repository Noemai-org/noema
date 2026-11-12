package inputgateway_test

// UND-02, core half (ruling A161; REQ-INP-001, REQ-INP-002, REQ-INP-003, REQ-AUD-001; INV-12, INV-11, EC-E16): the input gateway appends INPUT_RECEIVED to the Record
// before anything else happens, deduplicates a repeated request_id and refuses a reused one. All data is SYNTHETIC.

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/inputgateway"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

func body(requestID, text string) []byte {
	return []byte(`{"request_id":"` + requestID + `","channel":"text","text":"` + text + `","locale_hint":"en-NG"}`)
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// A161 item 1: a valid, session-bound input is appended as INPUT_RECEIVED by core-edge, for the session's tenant, with the payload the reconstruction reads.
func TestAValidInputIsRecordedAsInputReceivedByCoreEdgeBeforeTheTurnStarts(t *testing.T) {
	w := newWorld(t)
	sp := &spy{}
	g := w.gateway(tenantA, sp)
	res, err := g.Accept(ctx(t), identity(tenantA, "usr_88213"), body(reqID, "where is my money"))
	if err != nil {
		t.Fatal(err)
	}
	evs := w.events()
	if len(evs) != 1 || evs[0].EventType != "INPUT_RECEIVED" {
		t.Fatalf("the Record holds %+v, want exactly one INPUT_RECEIVED", evs)
	}
	e := evs[0]
	if e.WrittenBy != "core-edge" || e.Actor != "customer:usr_88213" || e.Body["tenant_id"] != tenantA {
		t.Errorf("written_by %q actor %q tenant %v", e.WrittenBy, e.Actor, e.Body["tenant_id"])
	}
	if got := keys(e.Payload); strings.Join(got, ",") != "channel,input_hash,locale,request_id" {
		t.Errorf("payload keys %v, want channel, input_hash, locale, request_id", got)
	}
	if e.Payload["channel"] != "text" || e.Payload["locale"] != "en-NG" || e.Payload["request_id"] != reqID {
		t.Errorf("payload %v", e.Payload)
	}
	if h, _ := e.Payload["input_hash"].(string); !strings.HasPrefix(h, "sha256:") || len(h) != 71 {
		t.Errorf("input_hash %q", h)
	}
	if strings.Contains(string(mustJSON(e.Payload)), "where is my money") {
		t.Error("the text of the input is in the Record payload")
	}
	// item 3: the orchestrator is handed the recorded event reference with the turn, and the event was already in the Record when it was.
	if len(sp.turns) != 1 || sp.received[0] != 1 {
		t.Fatalf("turns %d, INPUT_RECEIVED in the Record at that moment %v", len(sp.turns), sp.received)
	}
	turn := sp.turns[0]
	if turn.Input.EventID != e.EventID || turn.Input.Seq != e.Seq || turn.Input.Hash == "" || turn.CorrelationID != e.Corr || turn.RequestID != reqID || turn.Text != "where is my money" {
		t.Errorf("turn %+v does not carry the recorded event %s (seq %d, corr %s)", turn, e.EventID, e.Seq, e.Corr)
	}
	if turn.TenantID != tenantA || turn.PrincipalID != "usr_88213" || turn.SessionID != "ses_7a1b" {
		t.Errorf("turn identity %+v is not the session's", turn.Identity)
	}
	if res.Deduplicated || res.Input.EventID != e.EventID || res.CorrelationID != e.Corr {
		t.Errorf("result %+v", res)
	}
}

// A161 item 1, Record first: if the append fails the input is refused and nothing proceeds.
func TestWhenTheRecordRefusesTheEventTheInputIsRefusedAndNothingStarts(t *testing.T) {
	w := newWorld(t)
	sp := &spy{}
	g := w.gateway(tenantA, sp)
	w.fail.set(true)
	res, err := g.Accept(ctx(t), identity(tenantA, "usr_88213"), body(reqID, "where is my money"))
	if !errors.Is(err, inputgateway.ErrRecord) {
		t.Fatalf("err %v, want ErrRecord", err)
	}
	if res != (inputgateway.Result{}) || len(sp.turns) != 0 || len(w.events()) != 0 {
		t.Errorf("result %+v, turns %d, events %d: the refused input left something behind", res, len(sp.turns), len(w.events()))
	}
	// The same request after the Record recovers is a first request, not a duplicate of an input that never happened.
	w.fail.set(false)
	res, err = g.Accept(ctx(t), identity(tenantA, "usr_88213"), body(reqID, "where is my money"))
	if err != nil || res.Deduplicated || len(w.events("INPUT_RECEIVED")) != 1 || len(sp.turns) != 1 {
		t.Errorf("after recovery: result %+v err %v, INPUT_RECEIVED %d, turns %d", res, err, len(w.events("INPUT_RECEIVED")), len(sp.turns))
	}
}

// A161 item 2: the same request_id and the same body append INPUT_DEDUPLICATED, no second INPUT_RECEIVED, and return the original result.
func TestTheSameRequestIdAndBodyIsDeduplicatedAndReturnsTheOriginalResult(t *testing.T) {
	w := newWorld(t)
	sp := &spy{}
	g := w.gateway(tenantA, sp)
	first, err := g.Accept(ctx(t), identity(tenantA, "usr_88213"), body(reqID, "where is my money"))
	if err != nil {
		t.Fatal(err)
	}
	// The redelivery has other whitespace and another member order: it is the same body.
	again := []byte(`{ "locale_hint": "en-NG", "text": "where is my money", "channel": "text", "request_id": "` + reqID + `" }`)
	second, err := g.Accept(ctx(t), identity(tenantA, "usr_88213"), again)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Deduplicated || second.Input != first.Input || second.CorrelationID != first.CorrelationID {
		t.Errorf("second %+v, first %+v: the original result was not returned", second, first)
	}
	if n := len(w.events("INPUT_RECEIVED")); n != 1 {
		t.Errorf("%d INPUT_RECEIVED events, want 1", n)
	}
	dd := w.events("INPUT_DEDUPLICATED")
	if len(dd) != 1 {
		t.Fatalf("%d INPUT_DEDUPLICATED events, want 1", len(dd))
	}
	if dd[0].WrittenBy != "core-edge" || dd[0].Actor != "customer:usr_88213" || dd[0].Payload["request_id"] != reqID || dd[0].Payload["original_event_id"] != first.Input.EventID {
		t.Errorf("INPUT_DEDUPLICATED %+v", dd[0])
	}
	if len(sp.turns) != 1 {
		t.Errorf("the orchestrator was handed %d turns, want 1", len(sp.turns))
	}
}

// A161 item 2: the same request_id with another body is refused (409) and raises SECURITY_EVENT REQUEST_ID_REUSED; nothing else is appended.
func TestTheSameRequestIdWithAnotherBodyIsRefusedAndRaisesASecurityEvent(t *testing.T) {
	w := newWorld(t)
	sp := &spy{}
	g := w.gateway(tenantA, sp)
	first, err := g.Accept(ctx(t), identity(tenantA, "usr_88213"), body(reqID, "where is my money"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := g.Accept(ctx(t), identity(tenantA, "usr_88213"), body(reqID, "send it again"))
	if !errors.Is(err, inputgateway.ErrRequestIDReused) {
		t.Fatalf("err %v, want ErrRequestIDReused", err)
	}
	if res != (inputgateway.Result{}) {
		t.Errorf("result %+v on a refusal", res)
	}
	if n := len(w.events("INPUT_RECEIVED")); n != 1 {
		t.Errorf("%d INPUT_RECEIVED events, want 1", n)
	}
	sec := w.events("SECURITY_EVENT")
	if len(sec) != 1 || sec[0].Body["kind"] != "REQUEST_ID_REUSED" || sec[0].WrittenBy != "core-edge" {
		t.Fatalf("SECURITY_EVENT %+v, want one REQUEST_ID_REUSED written by core-edge", sec)
	}
	if sec[0].Payload["request_id"] != reqID || sec[0].Payload["original_event_id"] != first.Input.EventID {
		t.Errorf("security payload %v", sec[0].Payload)
	}
	if s := string(mustJSON(sec[0].Payload)); strings.Contains(s, "send it again") || strings.Contains(s, "where is my money") {
		t.Error("the text of an input is in the security event")
	}
	if len(w.events("INPUT_DEDUPLICATED")) != 0 || len(sp.turns) != 1 {
		t.Errorf("deduplicated %d, turns %d: a reused request id must not be treated as a retry", len(w.events("INPUT_DEDUPLICATED")), len(sp.turns))
	}
	// Still the same 409 when the security event cannot be appended: nothing proceeds either way.
	w.fail.set(true)
	_, err = g.Accept(ctx(t), identity(tenantA, "usr_88213"), body(reqID, "a third text"))
	if !errors.Is(err, inputgateway.ErrRequestIDReused) || !errors.Is(err, inputgateway.ErrRecord) || len(sp.turns) != 1 {
		t.Errorf("with the Record down: err %v, turns %d", err, len(sp.turns))
	}
}

// Record first, for a duplicate too: if INPUT_DEDUPLICATED cannot be appended the input is refused, not answered as if it had been recorded.
func TestWhenTheDeduplicationEventCannotBeAppendedTheRetryIsRefused(t *testing.T) {
	w := newWorld(t)
	sp := &spy{}
	g := w.gateway(tenantA, sp)
	if _, err := g.Accept(ctx(t), identity(tenantA, "usr_88213"), body(reqID, "where is my money")); err != nil {
		t.Fatal(err)
	}
	w.fail.set(true)
	res, err := g.Accept(ctx(t), identity(tenantA, "usr_88213"), body(reqID, "where is my money"))
	if !errors.Is(err, inputgateway.ErrRecord) || res != (inputgateway.Result{}) {
		t.Errorf("result %+v err %v, want ErrRecord and nothing", res, err)
	}
	if len(w.events("INPUT_DEDUPLICATED")) != 0 {
		t.Error("an INPUT_DEDUPLICATED event exists although the append failed")
	}
}

// REQ-INP-003 scope: a request id is checked for the same tenant and the same principal, and only within the window.
func TestARequestIdIsDeduplicatedOnlyForTheSamePrincipalAndTenantWithinTheWindow(t *testing.T) {
	w := newWorld(t)
	spA, spB := &spy{}, &spy{}
	gA, gB := w.gateway(tenantA, spA), w.gateway(tenantB, spB)
	if _, err := gA.Accept(ctx(t), identity(tenantA, "usr_88213"), body(reqID, "where is my money")); err != nil {
		t.Fatal(err)
	}
	for name, run := range map[string]func() (inputgateway.Result, error){
		"another principal of the tenant": func() (inputgateway.Result, error) {
			return gA.Accept(ctx(t), identity(tenantA, "usr_99999"), body(reqID, "where is my money"))
		},
		"another tenant": func() (inputgateway.Result, error) {
			return gB.Accept(ctx(t), identity(tenantB, "usr_88213"), body(reqID, "where is my money"))
		},
	} {
		res, err := run()
		if err != nil || res.Deduplicated {
			t.Errorf("%s: result %+v err %v, want a first request", name, res, err)
		}
	}
	w.clk.Advance(24*time.Hour + time.Minute)
	res, err := gA.Accept(ctx(t), identity(tenantA, "usr_88213"), body(reqID, "where is my money"))
	if err != nil || res.Deduplicated {
		t.Errorf("after the window: result %+v err %v, want a new input", res, err)
	}
	if n := len(w.events("INPUT_RECEIVED")); n != 4 {
		t.Errorf("%d INPUT_RECEIVED events, want 4", n)
	}
}

// Two redeliveries that arrive together are one input: the Record holds one INPUT_RECEIVED.
func TestConcurrentRedeliveriesProduceOneInputReceived(t *testing.T) {
	w := newWorld(t)
	sp := &spy{}
	g := w.gateway(tenantA, sp)
	var wg sync.WaitGroup
	results := make([]inputgateway.Result, 8)
	errs := make([]error, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = g.Accept(ctx(t), identity(tenantA, "usr_88213"), body(reqID, "where is my money"))
		}()
	}
	wg.Wait()
	dups := 0
	for i, err := range errs {
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		if results[i].Input != results[0].Input {
			t.Errorf("request %d got another reference", i)
		}
		if results[i].Deduplicated {
			dups++
		}
	}
	if n := len(w.events("INPUT_RECEIVED")); n != 1 || dups != 7 || len(sp.turns) != 1 {
		t.Errorf("INPUT_RECEIVED %d, deduplicated answers %d, turns %d; want 1, 7, 1", n, dups, len(sp.turns))
	}
}

// REQ-INP-001, REQ-INP-002, INV-12: what the gateway refuses before anything is written.
func TestWhatTheGatewayRefusesWritesNothingAndStartsNothing(t *testing.T) {
	w := newWorld(t)
	sp := &spy{}
	g := w.gateway(tenantA, sp)
	cases := map[string]string{
		"not json":                `nope`,
		"an array":                `[]`,
		"trailing data":           string(body(reqID, "hi")) + ` {}`,
		"a repeated member":       `{"request_id":"` + reqID + `","request_id":"` + reqID + `","channel":"text","text":"hi"}`,
		"an unknown member":       `{"request_id":"` + reqID + `","channel":"text","text":"hi","extra":1}`,
		"a tenant in the body":    `{"request_id":"` + reqID + `","channel":"text","text":"hi","tenant_id":"ten_bb"}`,
		"a principal in the body": `{"request_id":"` + reqID + `","channel":"text","text":"hi","principal_ref":"usr_1"}`,
		"a null member":           `{"request_id":"` + reqID + `","channel":"text","text":null}`,
		"a request id off shape":  `{"request_id":"REQ-1","channel":"text","text":"hi"}`,
		"an upper-case uuid":      `{"request_id":"0B8F3C1E-52A4-4D7E-9A61-3C0F5D2E7B19","channel":"text","text":"hi"}`,
		"a trailing newline":      `{"request_id":"` + reqID + "\\n" + `","channel":"text","text":"hi"}`,
		"a channel off the list":  `{"request_id":"` + reqID + `","channel":"email","text":"hi"}`,
		"no text":                 `{"request_id":"` + reqID + `","channel":"text"}`,
		"empty text":              `{"request_id":"` + reqID + `","channel":"text","text":""}`,
		"a locale off shape":      `{"request_id":"` + reqID + `","channel":"text","text":"hi","locale_hint":"EN_ng"}`,
		"a text that is a number": `{"request_id":"` + reqID + `","channel":"text","text":5}`,
	}
	for name, doc := range cases {
		res, err := g.Accept(ctx(t), identity(tenantA, "usr_88213"), []byte(doc))
		if !errors.Is(err, inputgateway.ErrInvalidInput) || res != (inputgateway.Result{}) {
			t.Errorf("%s: result %+v err %v, want ErrInvalidInput", name, res, err)
		}
	}
	for name, id := range map[string]inputgateway.Identity{
		"no tenant":                {PrincipalID: "usr_88213", PrincipalKind: "customer", SessionID: "ses_7a1b"},
		"no principal":             {TenantID: tenantA, PrincipalKind: "customer", SessionID: "ses_7a1b"},
		"no session":               {TenantID: tenantA, PrincipalID: "usr_88213", PrincipalKind: "customer"},
		"no kind":                  {TenantID: tenantA, PrincipalID: "usr_88213", SessionID: "ses_7a1b"},
		"a principal with a colon": {TenantID: tenantA, PrincipalID: "usr:1", PrincipalKind: "customer", SessionID: "ses_7a1b"},
	} {
		if _, err := g.Accept(ctx(t), id, body(reqID, "hi")); !errors.Is(err, inputgateway.ErrNoIdentity) {
			t.Errorf("%s: err %v, want ErrNoIdentity", name, err)
		}
	}
	if _, err := g.Accept(ctx(t), identity(tenantA, "usr_88213"), bytes.Repeat([]byte(" "), 64<<10+1)); !errors.Is(err, inputgateway.ErrInvalidInput) {
		t.Errorf("an oversize body: err %v", err)
	}
	if len(w.events()) != 0 || len(sp.turns) != 0 {
		t.Errorf("events %d, turns %d: a refused input left something behind", len(w.events()), len(sp.turns))
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func TestNewRefusesWhatItCannotRunWith(t *testing.T) {
	w := newWorld(t)
	sp := &spy{w: w}
	good := inputgateway.Config{
		Recorder: &inputgateway.RecordRecorder{Pool: w.edge[tenantA], Appender: &chain.Appender{Clock: w.clk, IDs: w.gen}, Timeout: time.Second},
		Index:    &inputgateway.RecordIndex{Pool: w.read[tenantA], Clock: w.clk, Window: time.Hour, Timeout: time.Second},
		Start:    sp.Start, IDs: w.gen, MaxBody: 1024,
	}
	if _, err := inputgateway.New(good); err != nil {
		t.Fatalf("the base config: %v", err)
	}
	for name, mutate := range map[string]func(c *inputgateway.Config){
		"no recorder":   func(c *inputgateway.Config) { c.Recorder = nil },
		"no index":      func(c *inputgateway.Config) { c.Index = nil },
		"no start":      func(c *inputgateway.Config) { c.Start = nil },
		"no id source":  func(c *inputgateway.Config) { c.IDs = nil },
		"no body bound": func(c *inputgateway.Config) { c.MaxBody = 0 },
	} {
		c := good
		mutate(&c)
		if _, err := inputgateway.New(c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// The HTTP edge of the gateway: identity from the session only, the error envelope of errors.v2, 409 for a reused request id.
func TestTheHandlerAnswersWithTheContractsStatusesAndNeverTakesIdentityFromTheRequest(t *testing.T) {
	w := newWorld(t)
	sp := &spy{}
	g := w.gateway(tenantA, sp)
	session := func(r *http.Request) (inputgateway.Identity, bool) {
		if r.Header.Get("X-Test-Session") == "" {
			return inputgateway.Identity{}, false
		}
		return identity(tenantA, "usr_88213"), true
	}
	h := g.Handler(session)
	post := func(b []byte, withSession bool, extra map[string]string) (*httptest.ResponseRecorder, map[string]any) {
		r := httptest.NewRequest(http.MethodPost, "/v1/conversations?tenant_id=ten_bb&principal=usr_1", bytes.NewReader(b))
		for k, v := range extra {
			r.Header.Set(k, v)
		}
		if withSession {
			r.Header.Set("X-Test-Session", "yes")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec, out
	}
	code := func(out map[string]any) string {
		e, _ := out["error"].(map[string]any)
		c, _ := e["code"].(string)
		return c
	}
	if rec, out := post(body(reqID, "hi"), false, nil); rec.Code != 401 || code(out) != "UNAUTHENTICATED" || len(w.events()) != 0 {
		t.Errorf("no session: %d %v, events %d", rec.Code, out, len(w.events()))
	}
	if rec, out := post([]byte(`{"nope"`), true, nil); rec.Code != 400 || code(out) != "INPUT_INVALID" {
		t.Errorf("malformed: %d %v", rec.Code, out)
	}
	rec, out := post(body(reqID, "where is my money"), true, map[string]string{"Noema-Tenant-Id": "ten_bb", "X-Principal": "operator:usr_9"})
	if rec.Code != 202 || out["deduplicated"] != false || out["correlation_id"] == "" {
		t.Fatalf("first: %d %v", rec.Code, out)
	}
	if evs := w.events("INPUT_RECEIVED"); len(evs) != 1 || evs[0].Body["tenant_id"] != tenantA || evs[0].Actor != "customer:usr_88213" {
		t.Errorf("the request's query and headers changed the identity: %+v", evs)
	}
	rec2, out2 := post(body(reqID, "where is my money"), true, nil)
	if rec2.Code != 200 || out2["deduplicated"] != true || out2["correlation_id"] != out["correlation_id"] || out2["input_event_id"] != out["input_event_id"] {
		t.Errorf("redelivery: %d %v, first %v", rec2.Code, out2, out)
	}
	rec3, out3 := post(body(reqID, "send it again"), true, nil)
	if rec3.Code != 409 || code(out3) != "REQUEST_ID_REUSED" {
		t.Errorf("reuse: %d %v", rec3.Code, out3)
	}
	w.fail.set(true)
	if rec, out := post(body("1c9a4d2f-63b5-4e8f-8b72-4d1a6e3f8c2a", "hi"), true, nil); rec.Code != 503 || code(out) != "DEPENDENCY_UNAVAILABLE" {
		t.Errorf("Record down: %d %v", rec.Code, out)
	}
	get := httptest.NewRequest(http.MethodGet, "/v1/conversations", nil)
	get.Header.Set("X-Test-Session", "yes")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, get)
	if rr.Code != 405 {
		t.Errorf("GET: %d, want 405", rr.Code)
	}
}

// project.go leaves a payload key that is missing out of the reconstruction, so an input with no locale_hint is recorded without a locale key, not with an empty one.
func TestAnInputWithoutALocaleHintIsRecordedWithoutALocaleKey(t *testing.T) {
	w := newWorld(t)
	g := w.gateway(tenantA, &spy{})
	doc := []byte(`{"request_id":"` + reqID + `","channel":"voice","text":"where is my money","voice":{"is_final":true}}`)
	if _, err := g.Accept(ctx(t), identity(tenantA, "usr_88213"), doc); err != nil {
		t.Fatal(err)
	}
	evs := w.events("INPUT_RECEIVED")
	if len(evs) != 1 {
		t.Fatalf("%d INPUT_RECEIVED events", len(evs))
	}
	if _, has := evs[0].Payload["locale"]; has || evs[0].Payload["channel"] != "voice" {
		t.Errorf("payload %v: want channel voice and no locale key", evs[0].Payload)
	}
}

// A161 item 1, "exactly as project.go reads them": a reconstruction built from the Record serves the recorded input in its REQUEST section with channel, locale and
// request_id, cites the INPUT_RECEIVED event, shows the later INPUT_DEDUPLICATED in the timeline, and serves neither the input hash nor the text.
func TestTheReconstructionServesTheRecordedInputAsItsRequestSection(t *testing.T) {
	w := newWorld(t)
	g := w.gateway(tenantA, &spy{})
	first, err := g.Accept(ctx(t), identity(tenantA, "usr_88213"), body(reqID, "where is my money"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Accept(ctx(t), identity(tenantA, "usr_88213"), body(reqID, "where is my money")); err != nil {
		t.Fatal(err)
	}
	raw, err := reconstruct.Build(ctx(t), w.read[tenantA], tenantA, first.CorrelationID)
	if err != nil {
		t.Fatalf("the Record the gateway wrote does not reconstruct: %v", err)
	}
	var out struct {
		Chain struct {
			Verified bool `json:"verified"`
		} `json:"chain"`
		Sections []struct {
			Kind  string `json:"kind"`
			Items []struct {
				RecordEventID string         `json:"record_event_id"`
				Basis         string         `json:"basis"`
				Facts         map[string]any `json:"facts"`
			} `json:"items"`
		} `json:"sections"`
		Timeline []struct {
			RecordEventID string `json:"record_event_id"`
			EventType     string `json:"event_type"`
		} `json:"timeline"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if !out.Chain.Verified {
		t.Error("the chain did not verify")
	}
	found := false
	for _, s := range out.Sections {
		if s.Kind != "REQUEST" {
			continue
		}
		for _, it := range s.Items {
			if it.RecordEventID != first.Input.EventID {
				continue
			}
			found = true
			if it.Basis != "PRINCIPAL_ACTION" || keysOf(it.Facts) != "channel,locale,request_id" {
				t.Errorf("REQUEST item: basis %q facts %v, want PRINCIPAL_ACTION with channel, locale and request_id", it.Basis, it.Facts)
			}
			if s := string(mustJSON(it.Facts)); !strings.Contains(s, reqID) || !strings.Contains(s, "en-NG") || strings.Contains(s, "where is my money") || strings.Contains(s, "sha256:") {
				t.Errorf("REQUEST facts %s", s)
			}
		}
	}
	if !found {
		t.Error("the REQUEST section does not cite the INPUT_RECEIVED event")
	}
	types := map[string]bool{}
	for _, e := range out.Timeline {
		types[e.EventType] = true
	}
	if !types["INPUT_RECEIVED"] || !types["INPUT_DEDUPLICATED"] {
		t.Errorf("timeline event types %v", types)
	}
}

func keysOf(m map[string]any) string { return strings.Join(keys(m), ",") }

// AGENT-13: the text of an input is never in an error, in the HTTP error envelope or in an answer, whatever the refusal.
func TestNoErrorOrResponseQuotesTheTextOfAnInput(t *testing.T) {
	const marker = "MARKER-7731-PRIVATE-TEXT"
	w := newWorld(t)
	sp := &spy{}
	g := w.gateway(tenantA, sp)
	h := g.Handler(func(*http.Request) (inputgateway.Identity, bool) { return identity(tenantA, "usr_88213"), true })
	first := body(reqID, marker)
	for name, doc := range map[string][]byte{
		"an unknown member":  []byte(`{"request_id":"` + reqID + `","channel":"text","text":"` + marker + `","extra":1}`),
		"a bad channel":      []byte(`{"request_id":"` + reqID + `","channel":"` + marker + `","text":"` + marker + `"}`),
		"a bad request id":   []byte(`{"request_id":"` + marker + `","channel":"text","text":"` + marker + `"}`),
		"a bad locale":       []byte(`{"request_id":"` + reqID + `","channel":"text","text":"` + marker + `","locale_hint":"` + marker + `"}`),
		"a first good input": first,
		"a redelivery":       first,
		"a reused id":        body(reqID, marker+" again"),
	} {
		if _, err := g.Accept(ctx(t), identity(tenantA, "usr_88213"), doc); err != nil && strings.Contains(err.Error(), marker) {
			t.Errorf("%s: the error quotes the text: %v", name, err)
		}
		r := httptest.NewRequest(http.MethodPost, "/v1/conversations", bytes.NewReader(doc))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if strings.Contains(rec.Body.String(), marker) {
			t.Errorf("%s: the HTTP answer quotes the text: %s", name, rec.Body.String())
		}
	}
	w.fail.set(true)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/conversations", bytes.NewReader(body("1c9a4d2f-63b5-4e8f-8b72-4d1a6e3f8c2a", marker))))
	if strings.Contains(rec.Body.String(), marker) {
		t.Errorf("Record down: the HTTP answer quotes the text: %s", rec.Body.String())
	}
	for _, e := range w.events() {
		if strings.Contains(string(mustJSON(e.Payload)), marker) || strings.Contains(string(mustJSON(e.Body)), marker) {
			t.Errorf("event %s %s quotes the text", e.EventType, e.EventID)
		}
	}
}
