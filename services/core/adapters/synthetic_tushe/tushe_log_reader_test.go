package synthetic_tushe_test

// The transition-log and event-feed reader (ruling A310; delta v1.3 section 3). SYNTHETIC. The fixtures hold exactly the members delta v1.3 names; the envelope of a list and the type of a field
// are not in any governing document, so the tests run the reader with each envelope shape and refuse everything else. No failure ANSWER is manufactured here: a status is classified by the
// package's own pure function, a connection refused and a stalled server are real transport behaviour, and a body the reader cannot read is a fixture, not a fault (AGENTS 8).

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/adapters/synthetic_tushe"
)

const readerSecret = "synthetic-reader-credential"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "tushe_v1_3", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type feedServer struct {
	mu      sync.Mutex
	methods []string
	paths   []string
	headers []http.Header
	body    func(r *http.Request) (int, string, []byte) // status, content type, body
}

func (f *feedServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.methods = append(f.methods, r.Method)
	f.paths = append(f.paths, r.URL.RequestURI())
	f.headers = append(f.headers, r.Header.Clone())
	f.mu.Unlock()
	status, ct, b := f.body(r)
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func newReader(t *testing.T, srv *feedServer, present synthetic_tushe.Presenter, shapes synthetic_tushe.ListShapes) *synthetic_tushe.TusheReader {
	t.Helper()
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	tok := filepath.Join(t.TempDir(), "cred")
	if err := os.WriteFile(tok, []byte(readerSecret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := synthetic_tushe.NewTusheReader(ts.URL, tok, present, shapes, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func deadline(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func asReadError(t *testing.T, err error) *adapters.ReadError {
	t.Helper()
	var re *adapters.ReadError
	if !errors.As(err, &re) {
		t.Fatalf("want a *ReadError, got %v", err)
	}
	if strings.Contains(re.Error(), readerSecret) {
		t.Fatal("the credential is in an error")
	}
	return re
}

func transitionsServer(body []byte) *feedServer {
	return &feedServer{body: func(*http.Request) (int, string, []byte) { return 200, "application/json", body }}
}

func TestTheTransitionLogIsReadWithEveryDocumentedMemberInEachEnvelopeShape(t *testing.T) {
	arr := fixture(t, "transitions.json")
	wrapped, _ := json.Marshal(map[string]json.RawMessage{"transitions": arr})
	for _, c := range []struct {
		name  string
		body  []byte
		shape synthetic_tushe.ListShape
	}{{"the body is the array", arr, synthetic_tushe.ListShape{}}, {"the array is a member", wrapped, synthetic_tushe.ListShape{Member: "transitions"}}} {
		t.Run(c.name, func(t *testing.T) {
			srv := transitionsServer(c.body)
			r := newReader(t, srv, synthetic_tushe.PresentBearer, synthetic_tushe.ListShapes{Transitions: c.shape})
			page, err := r.PayoutTransitions(deadline(t), "acct/1", "po 2")
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != 3 || page.RawHash == "" || !strings.HasPrefix(page.RawHash, "sha256:") {
				t.Fatalf("page %+v", page)
			}
			first, third := page.Items[0], page.Items[2]
			if first.Seq != 1 || first.FromState != "" || first.ToState != "CREATED" || first.RequestID != "syn-req-1" || first.IdempotencyKey != "syn-idem-1" ||
				!first.OccurredAt.Equal(time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)) || first.Inferred || first.Injected {
				t.Fatalf("first row %+v", first)
			}
			if third.Seq != 3 || third.FromState != "SUBMITTED" || third.ToState != "SETTLED" || !third.Inferred || third.Source != "processor" || third.Actor != "syn-actor-2" ||
				third.ObservedAt.Sub(third.OccurredAt) != 7500*time.Millisecond {
				t.Fatalf("third row %+v", third)
			}
			if srv.methods[0] != "GET" || srv.paths[0] != "/v1/accounts/acct%2F1/payouts/po%202/transitions" {
				t.Fatalf("request %s %s", srv.methods[0], srv.paths[0])
			}
		})
	}
}

func TestAnUnreadableTransitionLogIsUnknownNeverRowsOrNone(t *testing.T) {
	good := string(fixture(t, "transitions.json"))
	cases := map[string]string{
		"an unnamed member (drift)":           strings.Replace(good, `"actor": "syn-actor-1"`, `"actor": "syn-actor-1", "surprise": 1`, 1),
		"the sequence does not rise":          strings.Replace(good, `"seq": 2,`, `"seq": 1,`, 1),
		"the sequence falls":                  strings.Replace(good, `"seq": 3,`, `"seq": 0,`, 1),
		"a row without a sequence":            strings.Replace(good, `"seq": 2, `, ``, 1),
		"a row without a to-state":            strings.Replace(good, `"toState": "SUBMITTED"`, `"toState": ""`, 1),
		"an occurredAt that is not a time":    strings.Replace(good, `"occurredAt": "2026-11-01T09:00:05.000Z"`, `"occurredAt": "yesterday"`, 1),
		"an observedAt that is not a time":    strings.Replace(good, `"observedAt": "2026-11-01T09:00:05.050Z"`, `"observedAt": "soon"`, 1),
		"a sequence that is a string":         strings.Replace(good, `"seq": 2,`, `"seq": "2",`, 1),
		"JSON null where the array should be": `null`,
		"an object where an array was agreed": `{"transitions": []}`,
		"not JSON":                            `<html>maintenance</html>`,
		"trailing data after a row's JSON":    `[{"seq":1,"toState":"X","occurredAt":"2026-11-01T09:00:00Z"}] []`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			r := newReader(t, transitionsServer([]byte(body)), synthetic_tushe.PresentBearer, synthetic_tushe.ListShapes{})
			page, err := r.PayoutTransitions(deadline(t), "a", "p")
			if err == nil {
				t.Fatalf("read as %+v", page)
			}
			if re := asReadError(t, err); re.Failure != adapters.ReadMalformed || re.RawHash == "" {
				t.Fatalf("%+v", re)
			}
		})
	}
	t.Run("a body that is not served as JSON", func(t *testing.T) {
		srv := &feedServer{body: func(*http.Request) (int, string, []byte) { return 200, "text/html", []byte(good) }}
		r := newReader(t, srv, synthetic_tushe.PresentBearer, synthetic_tushe.ListShapes{})
		if _, err := r.PayoutTransitions(deadline(t), "a", "p"); asReadError(t, err).Failure != adapters.ReadMalformed {
			t.Fatal("a body served as HTML was read")
		}
	})
	t.Run("an empty array is an answer, not an error", func(t *testing.T) {
		r := newReader(t, transitionsServer([]byte(`[]`)), synthetic_tushe.PresentBearer, synthetic_tushe.ListShapes{})
		page, err := r.PayoutTransitions(deadline(t), "a", "p")
		if err != nil || len(page.Items) != 0 || page.RawHash == "" {
			t.Fatalf("%+v %v", page, err)
		}
	})
}

// feedBody serves the fixture's events with sequence greater than the `after` the request carries, at most `limit`.
func feedBody(t *testing.T, all []byte, wrapMember string) func(*http.Request) (int, string, []byte) {
	t.Helper()
	var events []map[string]any
	if err := json.Unmarshal(all, &events); err != nil {
		t.Fatal(err)
	}
	return func(r *http.Request) (int, string, []byte) {
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		page := []map[string]any{}
		for _, e := range events {
			if int64(e["sequence"].(float64)) > after && len(page) < limit {
				page = append(page, e)
			}
		}
		var b []byte
		if wrapMember == "" {
			b, _ = json.Marshal(page)
		} else {
			b, _ = json.Marshal(map[string]any{wrapMember: page})
		}
		return 200, "application/json", b
	}
}

func TestTheFeedIsReadForwardOnTheSequenceUntilItIsCaughtUp(t *testing.T) {
	for _, shape := range []synthetic_tushe.ListShape{{}, {Member: "events"}} {
		srv := &feedServer{body: feedBody(t, fixture(t, "events.json"), shape.Member)}
		r := newReader(t, srv, synthetic_tushe.PresentAPIKeyHeader, synthetic_tushe.ListShapes{Events: shape})
		events, err := r.ReadFeed(deadline(t), 0, 2, 10)
		if err != nil {
			t.Fatal(err)
		}
		var seqs []int64
		for _, e := range events {
			seqs = append(seqs, e.Sequence)
		}
		if len(events) != 5 || seqs[0] != 10 || seqs[4] != 20 || events[2].ID != "syn-evt-3" || events[2].TraceID != "syn-trace-2" {
			t.Fatalf("events %v", seqs)
		}
		if !strings.Contains(string(events[2].Raw), `"extra"`) || !strings.Contains(string(events[2].Raw), `"nested"`) {
			t.Fatalf("a member the delta does not name was dropped: %s", events[2].Raw)
		}
		// 5 events at 2 a page: 3 pages with events and one empty page that says it is caught up; the cursor is the last sequence, never a time
		if len(srv.paths) != 4 || srv.paths[0] != "/v1/events?after=0&limit=2" || srv.paths[1] != "/v1/events?after=11&limit=2" || srv.paths[3] != "/v1/events?after=20&limit=2" {
			t.Fatalf("requests %v", srv.paths)
		}
		for _, m := range srv.methods {
			if m != "GET" {
				t.Fatalf("method %s", m)
			}
		}
		if srv.headers[0].Get("X-API-Key") != readerSecret || srv.headers[0].Get("Authorization") != "" {
			t.Fatal("the credential was not presented the way the caller said")
		}
	}
}

func TestAFeedThatDoesNotAdvanceOrIsNotCaughtUpIsAnErrorNeverATruncatedResult(t *testing.T) {
	events := fixture(t, "events.json")
	t.Run("the page limit", func(t *testing.T) {
		r := newReader(t, &feedServer{body: feedBody(t, events, "")}, synthetic_tushe.PresentBearer, synthetic_tushe.ListShapes{})
		if _, err := r.ReadFeed(deadline(t), 0, 2, 2); asReadError(t, err).RawCode != "ADAPTER:FEED_PAGE_LIMIT" {
			t.Fatal("two pages of five events were returned as the whole feed")
		}
	})
	for name, replace := range map[string][2]string{
		"a repeated sequence":             {`"sequence": 11`, `"sequence": 10`},
		"a lower sequence":                {`"sequence": 15`, `"sequence": 9`},
		"an event without a trace id":     {`"traceId": "syn-trace-1", "type": "payout.submitted"`, `"type": "payout.submitted"`},
		"an event with a null trace id":   {`"traceId": "syn-trace-1"`, `"traceId": null`},
		"an event with an empty trace id": {`"traceId": "syn-trace-1"`, `"traceId": ""`},
		"an event without an id":          {`"id": "syn-evt-2", `, ``},
		"a sequence that is not a number": {`"sequence": 16`, `"sequence": "16"`},
		"a negative sequence":             {`"sequence": 20`, `"sequence": -1`},
	} {
		t.Run(name, func(t *testing.T) {
			body := strings.Replace(string(events), replace[0], replace[1], 1)
			srv := &feedServer{body: func(*http.Request) (int, string, []byte) { return 200, "application/json", []byte(body) }}
			r := newReader(t, srv, synthetic_tushe.PresentBearer, synthetic_tushe.ListShapes{})
			if _, err := r.EventsAfter(deadline(t), 0, 50); asReadError(t, err).Failure != adapters.ReadMalformed {
				t.Fatal("an unreadable page was read")
			}
		})
	}
}

func TestTheCredentialMustBePresentedAndIsNeverInAnErrorOrAFile(t *testing.T) {
	tok := filepath.Join(t.TempDir(), "cred")
	for name, content := range map[string]string{"empty": "\n", "two lines": "first\nsecond\n", "a space": "has space"} {
		if err := os.WriteFile(tok, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := synthetic_tushe.NewTusheReader("http://127.0.0.1:1", tok, synthetic_tushe.PresentBearer, synthetic_tushe.ListShapes{}, nil)
		if err == nil || strings.Contains(err.Error(), "first") || strings.Contains(err.Error(), "second") || strings.Contains(err.Error(), "has space") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := os.WriteFile(tok, []byte(readerSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := synthetic_tushe.NewTusheReader("http://127.0.0.1:1", tok, nil, synthetic_tushe.ListShapes{}, nil); err == nil {
		t.Error("a reader was built with no way to present the credential")
	}
	for _, bad := range []string{"ftp://host", "http://host/path", "http://host?x=1", "host", ""} {
		if _, err := synthetic_tushe.NewTusheReader(bad, tok, synthetic_tushe.PresentBearer, synthetic_tushe.ListShapes{}, nil); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestRealTransportFailuresAreUnavailableAndNoDeadlineIsInvalid(t *testing.T) {
	// A closed port: the connection is refused by the operating system. A server that accepts and never answers: the caller's deadline ends the read. Both are real behaviour.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	tok := filepath.Join(t.TempDir(), "cred")
	_ = os.WriteFile(tok, []byte(readerSecret), 0o600)
	stalled, err := synthetic_tushe.NewTusheReader("http://"+addr, tok, synthetic_tushe.PresentBearer, synthetic_tushe.ListShapes{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := stalled.PayoutTransitions(ctx, "a", "p"); asReadError(t, err).Failure != adapters.ReadUnavailable || asReadError(t, err).RawCode != "TIMEOUT" {
		t.Fatalf("a server that never answers: %v", err)
	}
	_ = l.Close()
	if _, err := stalled.EventsAfter(deadline(t), 0, 10); asReadError(t, err).Failure != adapters.ReadUnavailable {
		t.Fatalf("a refused connection: %v", err)
	}
	if _, err := stalled.EventsAfter(context.Background(), 0, 10); asReadError(t, err).RawCode != "ADAPTER:NO_DEADLINE" {
		t.Fatal("a read with no deadline was attempted")
	}
	if _, err := stalled.PayoutTransitions(deadline(t), "", "p"); asReadError(t, err).Failure != adapters.ReadInvalid {
		t.Fatal("an empty id was sent")
	}
	if _, err := stalled.EventsAfter(deadline(t), -1, 10); asReadError(t, err).Failure != adapters.ReadInvalid {
		t.Fatal("a negative cursor was sent")
	}
}

// rawServer is a real TCP server that answers each connection with the bytes given and then does what `after` says: nothing (the response stalls open), or a close. It is the real behaviour of
// a server that sends headers and then stalls or drops, not a manufactured status (AGENT-12). SYNTHETIC.
func rawServer(t *testing.T, response string, after func(c net.Conn, done <-chan struct{})) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	t.Cleanup(func() { close(done); _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				buf := make([]byte, 4096)
				_, _ = c.Read(buf)
				_, _ = c.Write([]byte(response))
				after(c, done)
			}()
		}
	}()
	return "http://" + l.Addr().String()
}

func TestAFailureWhileReadingTheBodyIsUnavailableNotMalformed(t *testing.T) {
	// Codex P2 on noema#711: a dependency that sent its headers and then stalled or dropped was reported as schema corruption (MALFORMED 200:TOO_LARGE_OR_CUT).
	head := "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 1000\r\n\r\n[{"
	tok := filepath.Join(t.TempDir(), "cred")
	_ = os.WriteFile(tok, []byte(readerSecret), 0o600)
	for name, tc := range map[string]struct {
		after func(c net.Conn, done <-chan struct{})
		want  string
		ctxMs time.Duration
	}{
		"headers then a stall until the deadline": {func(_ net.Conn, done <-chan struct{}) { <-done }, "TIMEOUT", 400 * time.Millisecond},
		"headers then a dropped connection":       {func(c net.Conn, _ <-chan struct{}) { _ = c.Close() }, "", 3000 * time.Millisecond},
	} {
		t.Run(name, func(t *testing.T) {
			base := rawServer(t, head, tc.after)
			r, err := synthetic_tushe.NewTusheReader(base, tok, synthetic_tushe.PresentBearer, synthetic_tushe.ListShapes{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), tc.ctxMs)
			defer cancel()
			for _, call := range []func() error{
				func() error { _, e := r.EventsAfter(ctx, 0, 10); return e },
				func() error { _, e := r.PayoutTransitions(ctx, "a", "p"); return e },
			} {
				re := asReadError(t, call())
				if re.Failure != adapters.ReadUnavailable {
					t.Fatalf("want UNAVAILABLE, got %v %q", re.Failure, re.RawCode)
				}
				if tc.want != "" && re.RawCode != tc.want {
					t.Fatalf("want %s, got %q", tc.want, re.RawCode)
				}
				if strings.Contains(re.RawCode, "TOO_LARGE_OR_CUT") {
					t.Fatal("a transport failure was classed as a cut body")
				}
			}
		})
	}
}
