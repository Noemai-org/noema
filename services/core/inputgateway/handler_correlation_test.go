package inputgateway_test

// Review P2 on noema#466: the error body of the handler carries the correlation ID of the request, the one the edge middleware put on the response header
// (PERD 39.1, session.CorrelationHeader), and never a second one of its own. No database: every case here fails before the Record is touched. SYNTHETIC.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/inputgateway"
)

type neverRecorder struct{}

func (neverRecorder) Received(context.Context, inputgateway.Identity, string, inputgateway.Received) (inputgateway.Ref, error) {
	return inputgateway.Ref{}, errors.New("not reached")
}
func (neverRecorder) Deduplicated(context.Context, inputgateway.Identity, string, string, inputgateway.Ref) error {
	return errors.New("not reached")
}
func (neverRecorder) Reused(context.Context, inputgateway.Identity, string, string, string, inputgateway.Ref) error {
	return errors.New("not reached")
}

type neverIndex struct{}

func (neverIndex) Find(context.Context, inputgateway.Identity, string) (inputgateway.Prior, bool, error) {
	return inputgateway.Prior{}, false, errors.New("not reached")
}

// brokenIDs is an ID source that always fails: the error path must not need it.
type brokenIDs struct{}

func (brokenIDs) ID(string) (string, error) { return "", errors.New("synthetic: no ids") }

var correlationShape = regexp.MustCompile(`^corr_[0-9A-Za-z]{2,32}$`)

// underMiddleware is what the edge's session middleware does to the response before the handler runs.
func underMiddleware(h http.Handler, header string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if header != "" {
			w.Header().Set(session.CorrelationHeader, header)
		}
		h.ServeHTTP(w, r)
	})
}

func errorBody(t *testing.T, rec *httptest.ResponseRecorder) (code, corr string) {
	t.Helper()
	var out struct {
		Error struct {
			Code          string `json:"code"`
			CorrelationID string `json:"correlation_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not an error envelope: %v: %s", err, rec.Body.String())
	}
	return out.Error.Code, out.Error.CorrelationID
}

func handlerOf(t *testing.T, resolved bool) http.Handler {
	t.Helper()
	g, err := inputgateway.New(inputgateway.Config{Recorder: neverRecorder{}, Index: neverIndex{}, IDs: brokenIDs{}, MaxBody: 64,
		Start: func(context.Context, inputgateway.Turn) error { return errors.New("not reached") }})
	if err != nil {
		t.Fatal(err)
	}
	return g.Handler(func(*http.Request) (inputgateway.Identity, bool) {
		return inputgateway.Identity{TenantID: "ten_aa", PrincipalID: "usr_88213", PrincipalKind: "customer", SessionID: "ses_7a1b"}, resolved
	})
}

func serve(h http.Handler, method, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, "/v1/conversations", strings.NewReader(body)))
	return rec
}

func TestEveryErrorBodyCarriesTheRequestsCorrelationIdFromTheMiddleware(t *testing.T) {
	const mine = "corr_REQ0001"
	cases := map[string]struct {
		h      http.Handler
		method string
		body   string
		status int
	}{
		"no session":       {handlerOf(t, false), http.MethodPost, `{}`, http.StatusUnauthorized},
		"an invalid body":  {handlerOf(t, true), http.MethodPost, `{"request_id":`, http.StatusBadRequest},
		"an oversize body": {handlerOf(t, true), http.MethodPost, strings.Repeat("x", 200), http.StatusRequestEntityTooLarge},
		"the wrong method": {handlerOf(t, true), http.MethodGet, ``, http.StatusMethodNotAllowed},
	}
	for name, c := range cases {
		rec := serve(underMiddleware(c.h, mine), c.method, c.body)
		code, corr := errorBody(t, rec)
		if rec.Code != c.status || code == "" {
			t.Errorf("%s: status %d code %q, want %d", name, rec.Code, code, c.status)
		}
		if corr != mine {
			t.Errorf("%s: the error body names correlation %q, the request's is %q", name, corr, mine)
		}
		if got := rec.Header().Get(session.CorrelationHeader); got != mine {
			t.Errorf("%s: the response header changed to %q", name, got)
		}
	}
}

func TestWithoutAUsableMiddlewareCorrelationTheBodyUsesTheEdgesOwnStandInAndStaysValid(t *testing.T) {
	for name, header := range map[string]string{"no header": "", "a malformed header": "not a correlation id", "an injected header": "corr_x\nSet-Cookie: a=b"} {
		h := underMiddleware(handlerOf(t, false), "")
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/conversations", strings.NewReader(`{}`))
		wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if header != "" {
				w.Header()[session.CorrelationHeader] = []string{header} // bypass canonicalisation checks: whatever a layer above wrote
			}
			h.ServeHTTP(w, r)
		})
		wrapped.ServeHTTP(rec, req)
		_, corr := errorBody(t, rec)
		if !correlationShape.MatchString(corr) {
			t.Errorf("%s: %q is not a valid correlation id (corr_ and 2 to 32 letters or digits)", name, corr)
		}
		if strings.ContainsAny(corr, "\n ") || strings.Contains(corr, "Cookie") {
			t.Errorf("%s: the body echoes %q", name, corr)
		}
	}
}
