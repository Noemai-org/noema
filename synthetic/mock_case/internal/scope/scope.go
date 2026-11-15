// Package scope binds a request to the tenant of the credential that authenticated it (Architect ruling R4, 8 Nov 2026; RESIST X6).
//
// mock-case takes `tenant_id` as a request parameter, so a credential alone says nothing about which tenant's data a request may touch. With one runtime
// credential per tenant, this middleware makes the credential the authority: a request whose `tenant_id` is not the credential's tenant never reaches a
// handler as a read or a write of that tenant. A read is served as a read of a tenant that has no data, so it looks exactly like a read of something that
// does not exist; a write is refused. SYNTHETIC (AGENT-25).
package scope

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// AbsentTenant is a tenant that holds no data: a wrong-tenant read runs as a read of it. It must match mock-case's tenant id syntax and must never be
// issued to a credential (the configuration refuses it).
const AbsentTenant = "ten_scope_absent"

// maxPeek bounds how much of a body is read to find the tenant. A larger body is passed through untouched and left to the handler to refuse.
const maxPeek = 1 << 20

// Wrap returns a handler that enforces the tenant of the credential. tenantOf reports the tenant bound to the request's credential, and false when the
// credential is not bound to one (the legacy single runtime credential), in which case nothing is changed. forbid answers a write that names another tenant.
func Wrap(next http.Handler, tenantOf func(*http.Request) (string, bool), forbid http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		own, bound := tenantOf(r)
		if !bound {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			q := r.URL.Query()
			if named := q.Get("tenant_id"); named != "" && named != own {
				q.Set("tenant_id", AbsentTenant)
				r2 := r.Clone(r.Context())
				r2.URL.RawQuery = q.Encode()
				next.ServeHTTP(w, r2)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		raw, rest := peek(r.Body)
		r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(raw), rest)) // the handler gets every byte back
		if named := tenantInBody(raw); named != "" && named != own {
			forbid(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// peek reads up to maxPeek bytes and returns them with the unread remainder.
func peek(body io.ReadCloser) ([]byte, io.Reader) {
	if body == nil {
		return nil, bytes.NewReader(nil)
	}
	raw, _ := io.ReadAll(io.LimitReader(body, maxPeek+1))
	if len(raw) > maxPeek { // too large to read whole: do not judge it here, hand it all on
		return raw, body
	}
	return raw, bytes.NewReader(nil)
}

// tenantInBody returns the `tenant_id` of a JSON object body, or "" if the body is not one, has none or was cut.
func tenantInBody(raw []byte) string {
	if len(raw) > maxPeek {
		return ""
	}
	var b struct {
		TenantID string `json:"tenant_id"`
	}
	if json.Unmarshal(raw, &b) != nil {
		return ""
	}
	return b.TenantID
}
