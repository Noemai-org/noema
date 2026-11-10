package session

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// identityKeys are the member names that carry an identity. Any JSON object, at any depth, with one of them (compared with Unicode simple case folding, as encoding/json matches field names) is
// refused: the principal, the tenant and the role come from the session only (ruling A1, INV-12, AGENT-10).
var identityKeys = map[string]bool{
	"principal_id": true, "principal": true, "tenant_id": true, "tenant": true, "sub": true, "iss": true, "roles": true, "role": true,
	"user_id": true, "user": true, "actor": true, "session": true, "token": true, "login": true,
}

// RefuseBodyIdentity is deny by default for request bodies (ruling A1: no identity from a request body). A request with a body is accepted only if
// the body is a single JSON document, read in full within maxBytes, with no member anywhere in it that names an identity; otherwise it is refused
// 400 INPUT_INVALID with a fixed message that does not repeat the body, and the handler never runs. An accepted body reaches the handler
// unchanged. A request without a body passes. Mount it inside Middleware, so an unauthenticated request never reaches it.
func RefuseBodyIdentity(ids IDs, maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			corr := correlationID(r, ids)
			w.Header().Set(CorrelationHeader, corr)
			if r.Body == nil || r.Body == http.NoBody {
				next.ServeHTTP(w, r)
				return
			}
			raw, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
			if err != nil || int64(len(raw)) > maxBytes {
				refuseBody(w, corr)
				return
			}
			if len(raw) == 0 {
				next.ServeHTTP(w, r)
				return
			}
			if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || !strings.EqualFold(mt, "application/json") {
				refuseBody(w, corr)
				return
			}
			if !plainJSON(raw) {
				refuseBody(w, corr)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(raw))
			next.ServeHTTP(w, r)
		})
	}
}

func refuseBody(w http.ResponseWriter, corr string) {
	writeErrorStatus(w, http.StatusBadRequest, contracts.ErrorsV2ErrorCodeINPUTINVALID, "The request is not acceptable.", corr)
}

// plainJSON reports whether raw is exactly one JSON document in which no object, at any depth, has a member named like an identity.
func plainJSON(raw []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return false
	}
	if _, err := dec.Token(); err != io.EOF {
		return false // a second document, or trailing bytes
	}
	return !namesIdentity(doc)
}

// namesIdentityKey compares with Unicode simple case folding, the folding encoding/json uses to match a member name to a struct field, so a
// spelling such as U+017F (long s) for "s" cannot reach a handler field the guard did not see.
func namesIdentityKey(k string) bool {
	for name := range identityKeys {
		if strings.EqualFold(k, name) {
			return true
		}
	}
	return false
}

func namesIdentity(v any) bool {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if namesIdentityKey(k) || namesIdentity(child) {
				return true
			}
		}
	case []any:
		for _, child := range t {
			if namesIdentity(child) {
				return true
			}
		}
	}
	return false
}
