package reconstruct

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

// Route is the path of REQ-AUD-003 (PERD 39.4), in net/http pattern form.
const Route = "GET /v1/reconstructions/{correlation_id}"

// RoleAudit is the only role this endpoint serves. REQ-AUD-003 ("Ops and audit role only") and the PERD 39.4 table
// ("audit") differ; until the founders resolve it, the narrower reading holds (AGENT-20) and the PR records the
// conflict.
const RoleAudit = "audit"

// Caller is the authenticated party, as the edge's session check establishes it (EDG-01). The tenant comes from the
// session and only from the session (INV-12): the request path never names it.
type Caller struct {
	TenantID  string
	Principal string // "<kind>:<id>", for the access log
	Roles     []string
}

// Authorizer authenticates a request. EDG-01 supplies the real one when it wires this handler into cmd/core.
type Authorizer interface {
	Authenticate(r *http.Request) (Caller, error)
}

// ErrUnauthenticated is what DenyAll returns for every request.
var ErrUnauthenticated = errors.New("reconstruct: no authenticated session")

// DenyAll is the default Authorizer: it refuses every request. Nothing reaches the Record until EDG-01 replaces it.
type DenyAll struct{}

// Authenticate refuses.
func (DenyAll) Authenticate(*http.Request) (Caller, error) { return Caller{}, ErrUnauthenticated }

// IDSource makes the correlation ID of an error answer when the request named no valid one.
type IDSource interface {
	ID(prefix string) (string, error)
}

// Handler serves REQ-AUD-003. It is not wired into cmd/core (EDG-01 does that, approved escalation E2 of REC-03).
type Handler struct {
	DB      TxBeginner // connections run as record_read
	Auth    Authorizer // nil means DenyAll
	IDs     IDSource
	Log     *slog.Logger  // the access log: RECORD_ACCESSED lines, never the chain
	Timeout time.Duration // the deadline for one reconstruction; zero means DefaultTimeout
}

// DefaultTimeout bounds one reconstruction. REQ-AUD-003 asks P95 < 1 s for 200 events [REC]; the deadline is wider
// so that a slow read fails as DEPENDENCY_UNAVAILABLE instead of hanging.
const DefaultTimeout = 5 * time.Second

var correlationRe = regexp.MustCompile(`^corr_[0-9A-Za-z]{2,32}$`)

// Error codes of contracts/schemas/errors.v2.json this handler answers with, and whether each is retryable there.
const (
	codeUnauthenticated = "UNAUTHENTICATED"
	codeNotFound        = "NOT_FOUND_OR_NOT_AUTHORIZED"
	codeInputInvalid    = "INPUT_INVALID"
	codeIntegrity       = "RECORD_INTEGRITY_FAILURE"
	codeSafetyBlock     = "INTERNAL_SAFETY_BLOCK"
	codeDependency      = "DEPENDENCY_UNAVAILABLE"
)

var retryable = map[string]bool{codeDependency: true}

const headerCorrelation = "Noema-Correlation-Id"

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	auth := h.Auth
	if auth == nil {
		auth = DenyAll{}
	}
	corr := r.PathValue("correlation_id")
	if !correlationRe.MatchString(corr) {
		corr = ""
	}
	caller, err := auth.Authenticate(r)
	if err != nil {
		h.access(r.Context(), Caller{}, corr, codeUnauthenticated)
		h.fail(w, http.StatusUnauthorized, codeUnauthenticated, "Authentication is required.", corr)
		return
	}
	if !hasRole(caller.Roles, RoleAudit) {
		// The same code as not found, so a caller cannot learn that a correlation exists (errors.v2.json).
		h.access(r.Context(), caller, corr, codeNotFound)
		h.fail(w, http.StatusNotFound, codeNotFound, "Not found.", corr)
		return
	}
	if corr == "" {
		h.access(r.Context(), caller, "", codeInputInvalid)
		h.fail(w, http.StatusBadRequest, codeInputInvalid, "The correlation ID is not valid.", "")
		return
	}
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	body, err := Build(ctx, h.DB, caller.TenantID, corr)
	switch {
	case err == nil:
		h.access(ctx, caller, corr, "SERVED")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set(headerCorrelation, corr)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	case errors.Is(err, ErrIntegrity):
		h.access(ctx, caller, corr, codeIntegrity)
		h.fail(w, http.StatusConflict, codeIntegrity, "The Record did not verify, so it is not served.", corr)
	case errors.Is(err, ErrNotFound):
		h.access(ctx, caller, corr, codeNotFound)
		h.fail(w, http.StatusNotFound, codeNotFound, "Not found.", corr)
	case errors.Is(err, ErrUnprojectable):
		h.access(ctx, caller, corr, codeSafetyBlock)
		h.fail(w, http.StatusInternalServerError, codeSafetyBlock, "The Record holds an object that cannot be served.", corr)
	default:
		h.access(ctx, caller, corr, codeDependency)
		h.fail(w, http.StatusServiceUnavailable, codeDependency, "The Record is not available.", corr)
	}
}

func hasRole(roles []string, want string) bool {
	for _, r := range roles {
		if r == want {
			return true
		}
	}
	return false
}

// access writes the RECORD_ACCESSED line (REQ-AUD-003: reads are logged in the access log, not the chain). It carries
// IDs and codes only, never Record content.
func (h *Handler) access(ctx context.Context, c Caller, corr, outcome string) {
	if h.Log == nil {
		return
	}
	h.Log.LogAttrs(ctx, slog.LevelInfo, "RECORD_ACCESSED", slog.String("tenant_id", c.TenantID), slog.String("principal", c.Principal),
		slog.String("correlation_id", corr), slog.String("outcome", outcome))
}

// fail writes the error envelope of errors.v2.json. Every envelope carries a correlation ID: the request's when it was
// valid, otherwise a fresh one.
func (h *Handler) fail(w http.ResponseWriter, status int, code, message, corr string) {
	if corr == "" && h.IDs != nil {
		if id, err := h.IDs.ID("corr"); err == nil {
			corr = id
		}
	}
	body, err := crypto.CanonicalJSON(map[string]any{"error": map[string]any{"code": code, "message": message, "retryable": retryable[code], "correlation_id": corr}})
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if corr != "" {
		w.Header().Set(headerCorrelation, corr)
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
