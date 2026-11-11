package session

import (
	"context"
	"encoding/json"
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
			w.Header().Set(CorrelationHeader, corr)
			authz := r.Header.Get("Authorization")
			if authz == "" {
				writeError(w, contracts.ErrorsV2ErrorCodeUNAUTHENTICATED, "A session is required.", corr)
				return
			}
			token, ok := strings.CutPrefix(authz, "Bearer ")
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

// correlationID keeps a well-formed incoming ID so a call can be traced end to end, and makes a new one otherwise.
func correlationID(r *http.Request, ids IDs) string {
	if in := r.Header.Get(CorrelationHeader); correlationShape.MatchString(in) {
		return in
	}
	id, err := ids.ID("corr")
	if err != nil || !correlationShape.MatchString(id) {
		return noCorrelationID
	}
	return id
}

func writeError(w http.ResponseWriter, code contracts.ErrorsV2ErrorCode, message, corr string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	body := contracts.ErrorsV2ErrorEnvelope{Error: contracts.ErrorsV2ErrorBody{Code: code, Message: message, Retryable: false, CorrelationId: contracts.CommonV1CorrelationId(corr)}}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Warn("error response not written", "correlation_id", corr)
	}
}
