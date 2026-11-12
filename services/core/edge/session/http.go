package session

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// CorrelationHeader carries the correlation ID on every response (PERD 39.1).
const CorrelationHeader = "Noema-Correlation-Id"

// noCorrelationID stands in only when the ID source fails; it still has the contract's shape.
const noCorrelationID = "corr_NONE"

var correlationShape = regexp.MustCompile(`^corr_[0-9A-Za-z]{2,32}$`)

// IDs makes correlation IDs.
type IDs interface {
	ID(prefix string) (string, error)
}

type ctxKey struct{}

type corrKey struct{}

// CorrelationFromContext returns the correlation ID the edge minted for this request, or "" for a request that passed no session check. Later layers (the role gate,
// the body guard, handlers, the Record's correlation chain) use this one ID; none takes it from the request.
func CorrelationFromContext(ctx context.Context) string {
	c, _ := ctx.Value(corrKey{}).(string)
	return c
}

// FromContext returns the authenticated principal of a request that passed Middleware.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}

// Middleware requires a valid bearer session on every request (REQ-API-004). A missing credential is 401 UNAUTHENTICATED, any other
// failure is 401 SESSION_INVALID with one fixed message, so a forged token and an expired one look the same (REQ-API-002). The handler
// runs only for an authenticated request and finds the principal in its context; no header other than the token names one (INV-12).
func Middleware(v *Verifier, ids IDs) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			corr := correlationID(r, ids)
			r = r.WithContext(context.WithValue(r.Context(), corrKey{}, corr))
			w.Header().Set(CorrelationHeader, corr)
			authz := r.Header.Get("Authorization")
			if authz == "" {
				writeError(w, contracts.ErrorsV2ErrorCodeUNAUTHENTICATED, "A session is required.", corr)
				return
			}
			token, ok := bearerToken(authz)
			if !ok {
				writeError(w, contracts.ErrorsV2ErrorCodeSESSIONINVALID, "The session is not valid.", corr)
				return
			}
			p, err := v.Verify(token)
			if err != nil {
				slog.Warn("session rejected", "correlation_id", corr)
				writeError(w, contracts.ErrorsV2ErrorCodeSESSIONINVALID, "The session is not valid.", corr)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, p)))
		})
	}
}

// RequireRole lets a request through only if its session carries one of the allowed roles (ruling A1). It is deny by default: a request with
// no principal in its context, or a gate with no allowed roles, is refused. A role that is not allowed gets the answer for "not found", so a
// caller cannot tell a hidden route from a forbidden one (REQ-API-002). Mount it inside Middleware.
func RequireRole(ids IDs, allowed ...Role) func(http.Handler) http.Handler {
	allow := make(map[Role]bool, len(allowed))
	for _, r := range allowed {
		allow[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			corr := correlationID(r, ids)
			w.Header().Set(CorrelationHeader, corr)
			p, ok := FromContext(r.Context())
			if !ok {
				writeError(w, contracts.ErrorsV2ErrorCodeUNAUTHENTICATED, "A session is required.", corr)
				return
			}
			if !allow[p.Role] {
				writeErrorStatus(w, http.StatusNotFound, contracts.ErrorsV2ErrorCodeNOTFOUNDORNOTAUTHORIZED, "Not found.", corr)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ErrTenantMismatch is returned when a principal's tenant is not the tenant of the database login its request would run on.
var ErrTenantMismatch = errors.New("session: the token's tenant is not the tenant of the database login")

// CheckTenant refuses a principal whose tenant differs from loginTenant, the tenant the database maps the request's login to (DEBT-04).
// Empty never matches empty. The ops role acts in its own tenant only, until a decision says otherwise (ruling A1, item 7; REQ-SEC-006).
func CheckTenant(p Principal, loginTenant string) error {
	if p.Tenant == "" || loginTenant == "" || p.Tenant != loginTenant {
		return ErrTenantMismatch
	}
	return nil
}

// bearerToken takes the credential of "<scheme> <credential>" when the scheme is Bearer. The scheme name is case-insensitive (RFC 9110
// section 11.1); exactly one space separates it from a non-empty credential, which is not changed.
func bearerToken(authz string) (string, bool) {
	scheme, cred, ok := strings.Cut(authz, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || cred == "" || strings.HasPrefix(cred, " ") {
		return "", false
	}
	return cred, true
}

// correlationID is the request's correlation ID: the one the session check put in the context, or a new one when this layer runs without it. An incoming
// Noema-Correlation-Id is never adopted: a caller could send an account-number-shaped value (corr_0123456789) and have it echoed in the answer and carried into logs and
// the Record with the chain (noema#458, REQ-SEC-003). The chain ID is ours; the caller's value is not kept.
func correlationID(r *http.Request, ids IDs) string {
	if c := CorrelationFromContext(r.Context()); correlationShape.MatchString(c) {
		return c
	}
	id, err := ids.ID("corr")
	if err != nil || !correlationShape.MatchString(id) {
		return noCorrelationID
	}
	return id
}

func writeError(w http.ResponseWriter, code contracts.ErrorsV2ErrorCode, message, corr string) {
	writeErrorStatus(w, http.StatusUnauthorized, code, message, corr)
}

func writeErrorStatus(w http.ResponseWriter, status int, code contracts.ErrorsV2ErrorCode, message, corr string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := contracts.ErrorsV2ErrorEnvelope{Error: contracts.ErrorsV2ErrorBody{Code: code, Message: message, Retryable: false, CorrelationId: contracts.CommonV1CorrelationId(corr)}}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Warn("error response not written", "correlation_id", corr)
	}
}
