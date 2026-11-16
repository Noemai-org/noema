package understanding_test

// UND-04, core half (REQ-INT-001, REQ-INT-002): the intent client. All data is SYNTHETIC.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/understanding"
)

const input = `{"schema_version":"2.0","text":"where is my money"}`

func deadline(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func serve(t *testing.T, h http.HandlerFunc) *understanding.Client {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	c, err := understanding.NewClient(understanding.ClientConfig{BaseURL: s.URL, HTTP: s.Client(), MaxResponseBytes: 1 << 16})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func envelope(code string) string {
	return `{"error":{"code":"` + code + `","message":"m","retryable":false,"correlation_id":"corr_4c21f9"}}`
}

func failure(t *testing.T, err error) understanding.Failure {
	t.Helper()
	ce, ok := err.(*understanding.CompileError)
	if !ok {
		t.Fatalf("error %v is not a *CompileError", err)
	}
	return ce.Failure
}

func TestCompileSendsTheContractsBodyAndReturnsBothObjects(t *testing.T) {
	var got map[string]json.RawMessage
	var path, method string
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"intent_object":{"a":1},"canonical_input":{"b":2}}`))
	})
	out, err := c.Compile(deadline(t), understanding.CompileRequest{CanonicalInput: json.RawMessage(input), PackRef: "fin@1.0.0"})
	if err != nil || string(out.IntentObject) != `{"a":1}` || string(out.CanonicalInput) != `{"b":2}` {
		t.Fatalf("out %+v err %v", out, err)
	}
	if method != http.MethodPost || path != "/internal/v1/understanding:compile" {
		t.Errorf("%s %s", method, path)
	}
	if _, has := got["open_intent"]; has || string(got["pack_ref"]) != `"fin@1.0.0"` || string(got["canonical_input"]) != input {
		t.Errorf("body %v", got)
	}
	_, err = c.Compile(deadline(t), understanding.CompileRequest{CanonicalInput: json.RawMessage(input), OpenIntent: json.RawMessage(`{"x":1}`), PackRef: "fin@1.0.0"})
	if err != nil || string(got["open_intent"]) != `{"x":1}` {
		t.Errorf("open_intent not sent: %v %v", got, err)
	}
}

func TestEveryAnswerThatIsNotTheContractsIsAFailureNamedForWhatItMeans(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   understanding.Failure
	}{
		{"every provider failed", 502, envelope("PROVIDER_UNAVAILABLE"), understanding.FailureProviderUnavailable},
		{"the input was refused", 422, envelope("INPUT_INVALID"), understanding.FailureInputInvalid},
		{"a server error with no envelope", 500, "oops", understanding.FailureUnavailable},
		{"a client error with no envelope", 400, "oops", understanding.FailureMalformed},
		{"an envelope with another code and a client status", 400, envelope("WRITES_DISABLED"), understanding.FailureMalformed},
		{"an envelope with an unknown member", 502, `{"error":{"code":"PROVIDER_UNAVAILABLE","message":"m","retryable":false,"correlation_id":"corr_1","extra":1}}`, understanding.FailureUnavailable},
		{"a 200 with an unknown member", 200, `{"intent_object":{},"canonical_input":{},"extra":1}`, understanding.FailureMalformed},
		{"a 200 with a missing member", 200, `{"intent_object":{}}`, understanding.FailureMalformed},
		{"a 200 with a null member", 200, `{"intent_object":null,"canonical_input":{}}`, understanding.FailureMalformed},
		{"a 200 with a repeated member", 200, `{"intent_object":{},"intent_object":{},"canonical_input":{}}`, understanding.FailureMalformed},
		{"a 200 with trailing data", 200, `{"intent_object":{},"canonical_input":{}} {}`, understanding.FailureMalformed},
		{"a 200 that is not json", 200, "ok", understanding.FailureMalformed},
		{"a 200 with an empty body", 200, "", understanding.FailureMalformed},
		{"a 204", 204, "", understanding.FailureMalformed},
	}
	for _, tc := range cases {
		c := serve(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		})
		_, err := c.Compile(deadline(t), understanding.CompileRequest{CanonicalInput: json.RawMessage(input), PackRef: "fin@1.0.0"})
		if err == nil || failure(t, err) != tc.want {
			t.Errorf("%s: %v, want %s", tc.name, err, tc.want)
		}
	}
}

func TestAnAnswerLongerThanTheBoundIsMalformedAndNotRead(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"intent_object":"` + strings.Repeat("a", 4096) + `","canonical_input":{}}`))
	}))
	defer s.Close()
	c, _ := understanding.NewClient(understanding.ClientConfig{BaseURL: s.URL, HTTP: s.Client(), MaxResponseBytes: 1024})
	_, err := c.Compile(deadline(t), understanding.CompileRequest{CanonicalInput: json.RawMessage(input), PackRef: "fin@1.0.0"})
	if err == nil || failure(t, err) != understanding.FailureMalformed {
		t.Fatalf("%v", err)
	}
}

func TestNothingIsSentWithoutADeadlineOrWithAMalformedRequest(t *testing.T) {
	var calls int32
	c := serve(t, func(w http.ResponseWriter, _ *http.Request) { atomic.AddInt32(&calls, 1); _, _ = w.Write([]byte(`{}`)) })
	for name, tc := range map[string]struct {
		ctx context.Context
		req understanding.CompileRequest
	}{
		"no deadline":             {context.Background(), understanding.CompileRequest{CanonicalInput: json.RawMessage(input), PackRef: "fin@1.0.0"}},
		"a pack ref off shape":    {deadline(t), understanding.CompileRequest{CanonicalInput: json.RawMessage(input), PackRef: "fin"}},
		"an input not json":       {deadline(t), understanding.CompileRequest{CanonicalInput: json.RawMessage("{"), PackRef: "fin@1.0.0"}},
		"no input":                {deadline(t), understanding.CompileRequest{PackRef: "fin@1.0.0"}},
		"an open intent not json": {deadline(t), understanding.CompileRequest{CanonicalInput: json.RawMessage(input), OpenIntent: json.RawMessage("{"), PackRef: "fin@1.0.0"}},
	} {
		if _, err := c.Compile(tc.ctx, tc.req); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Errorf("%d calls reached the server", calls)
	}
}

func TestARedirectIsNotFollowedSoTheBodyGoesToNoOtherHost(t *testing.T) {
	var elsewhere int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { atomic.AddInt32(&elsewhere, 1) }))
	defer other.Close()
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	})
	_, err := c.Compile(deadline(t), understanding.CompileRequest{CanonicalInput: json.RawMessage(input), PackRef: "fin@1.0.0"})
	if err == nil || atomic.LoadInt32(&elsewhere) != 0 {
		t.Fatalf("err %v, calls to the other host %d", err, elsewhere)
	}
}

func TestAnUnreachableOrSlowServiceIsUnavailable(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.Copy(io.Discard, r.Body); <-r.Context().Done() })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := c.Compile(ctx, understanding.CompileRequest{CanonicalInput: json.RawMessage(input), PackRef: "fin@1.0.0"})
	if err == nil || failure(t, err) != understanding.FailureUnavailable {
		t.Fatalf("%v", err)
	}
	if strings.Contains(err.Error(), "where is my money") {
		t.Error("the error quotes the request")
	}
}

func TestNewClientRefusesWhatItCannotRunWith(t *testing.T) {
	for name, cfg := range map[string]understanding.ClientConfig{
		"a path in the url": {BaseURL: "http://model:8080/internal", HTTP: http.DefaultClient, MaxResponseBytes: 1},
		"a query":           {BaseURL: "http://model:8080?x=1", HTTP: http.DefaultClient, MaxResponseBytes: 1},
		"another scheme":    {BaseURL: "ftp://model", HTTP: http.DefaultClient, MaxResponseBytes: 1},
		"no host":           {BaseURL: "http://", HTTP: http.DefaultClient, MaxResponseBytes: 1},
		"no http client":    {BaseURL: "http://model:8080", MaxResponseBytes: 1},
		"no response bound": {BaseURL: "http://model:8080", HTTP: http.DefaultClient},
	} {
		if _, err := understanding.NewClient(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
