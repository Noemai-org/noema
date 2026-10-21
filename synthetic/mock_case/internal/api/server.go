// Package api serves mock-case: the case API for the core's case adapter (tushe_net) and the truth and control API
// for the benchmark runner (bench_net). They are two handlers on two listeners, each behind its own credential.
// Operations the case system must not offer (close, resolve, downgrade priority, change an SLA or clock, edit or
// delete evidence) have no route, no handler and no entry in either API description (REQ-ENV-008).
package api

import (
	_ "embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Noemai-org/noema/synthetic/mock_case/internal/auth"
	"github.com/Noemai-org/noema/synthetic/mock_case/internal/clock"
	"github.com/Noemai-org/noema/synthetic/mock_case/internal/config"
	"github.com/Noemai-org/noema/synthetic/mock_case/internal/faultplan"
	"github.com/Noemai-org/noema/synthetic/mock_case/internal/store"
)

//go:embed openapi.json
var caseDescription []byte

//go:embed openapi-bench.json
var benchDescription []byte

// CaseDescription and BenchDescription are the API descriptions the two listeners serve.
func CaseDescription() []byte  { return caseDescription }
func BenchDescription() []byte { return benchDescription }

const maxBody = 1 << 20 // transport sanity bound on a request body

// Server holds everything a request needs. Create it with New.
type Server struct {
	cfg    *config.Config
	st     *store.Store
	engine *faultplan.Engine
	auth   *auth.Authenticator
	log    *slog.Logger

	clkMu sync.Mutex
	clk   clock.Clock
	sim   *clock.Simulated
}

// New returns a Server on the wall clock with no fault plan.
func New(cfg *config.Config, st *store.Store, log *slog.Logger) *Server {
	return &Server{cfg: cfg, st: st, engine: faultplan.NewEngine(), auth: auth.New(cfg.RuntimeToken, cfg.BenchToken), log: log, clk: clock.System{}}
}

func (s *Server) now() (time.Time, bool) {
	s.clkMu.Lock()
	defer s.clkMu.Unlock()
	return s.clk.Now(), s.clk.Simulated()
}

func (s *Server) advance(by time.Duration) error {
	s.clkMu.Lock()
	defer s.clkMu.Unlock()
	if s.sim == nil {
		return errNotSimulated
	}
	return s.sim.Advance(by)
}

var errNotSimulated = errors.New("the clock is not simulated: load a fault plan with a clock block first")

// ---- responses ----

type errBody struct {
	Error errPayload `json:"error"`
}

type errPayload struct {
	Code          string      `json:"code"`
	Message       string      `json:"message"`
	Retryable     bool        `json:"retryable"`
	CorrelationID string      `json:"correlation_id,omitempty"`
	Details       []errDetail `json:"details,omitempty"`
}

type errDetail struct {
	Field string `json:"field"`
	Issue string `json:"issue"`
}

// apiError is a refusal with its HTTP status.
type apiError struct {
	Status int
	errPayload
	RetryAfter *int
}

func newErr(status int, code, message string, retryable bool, details ...errDetail) *apiError {
	return &apiError{Status: status, errPayload: errPayload{Code: code, Message: message, Retryable: retryable, Details: details}}
}

func badRequest(field, issue string) *apiError {
	return newErr(400, "INVALID_REQUEST", "the request is not valid", false, errDetail{field, issue})
}

func setCommon(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Mock-Case-Synthetic", "true") // AGENT-25: every response from this service is synthetic
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	setCommon(w)
	body, err := json.Marshal(v)
	if err != nil {
		status, body = 500, []byte(`{"error":{"code":"INTERNAL","message":"could not encode the response","retryable":false}}`)
	}
	w.WriteHeader(status)
	w.Write(append(body, '\n')) //nolint:errcheck
}

func writeErr(w http.ResponseWriter, corr string, e *apiError) {
	e.CorrelationID = corr
	if e.RetryAfter != nil {
		w.Header().Set("Retry-After", strconv.Itoa(*e.RetryAfter))
	}
	writeJSON(w, e.Status, errBody{e.errPayload})
}

// ---- authentication ----

// gate rejects every request that does not carry the expected identity, before routing: a caller with the wrong
// credential learns nothing about which paths exist. /healthz is the one open path of the case listener.
func (s *Server) gate(want auth.Identity, open map[string]bool, next http.Handler, listener string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if open[r.URL.Path] && r.Method == http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		got := s.auth.Identify(r)
		if got != want {
			status, code, msg := 401, "UNAUTHENTICATED", "a valid credential is required"
			if got != auth.None {
				status, code, msg = 403, "FORBIDDEN", "this credential does not open this API"
			}
			// Every refusal on the bench listener is an oracle-access attempt worth seeing (REQ-ENV-002).
			s.log.Warn("refused", "listener", listener, "identity", got.String(), "method", r.Method, "path", r.URL.Path, "status", status)
			writeErr(w, correlationID(r), newErr(status, code, msg, false))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func correlationID(r *http.Request) string {
	v := r.Header.Get("Noema-Correlation-Id")
	if len(v) > 64 || !corrRE.MatchString(v) {
		return ""
	}
	return v
}
