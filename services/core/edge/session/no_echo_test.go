package session_test

// SEC-03 (REQ-SEC-003, AGENT-13): API output never echoes what the caller sent. A full account number planted in the path, the query, a header, the bearer token or the body of a
// request that is refused does not come back in the response body or any response header: the refusal messages are fixed text. All data is SYNTHETIC.

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

var digitRun = regexp.MustCompile(`[0-9]{5,}`)

// typedID is the one exemption that can appear in a response: the correlation ID the core minted (A154: typed IDs <prefix>_<ULID> are exempt; a random ULID can hold a run of five digits).
var typedID = regexp.MustCompile(`[a-z]{2,6}_[0-7][0-9A-HJKMNP-TV-Z]{25}`)

func TestAnAccountNumberInARefusedRequestIsNotEchoedInTheResponse(t *testing.T) {
	const acct = "0123456789"
	k := newKeys(t)
	h, calls, _ := guarded(t, k)
	good := "Bearer " + mint(t, k.priv, goodHeader(), goodClaims(epoch))
	type tc struct {
		name   string
		method string
		target string
		header map[string]string
		body   string
	}
	for _, c := range []tc{
		{"path, no credential", "GET", "/v1/accounts/" + acct, nil, ""},
		{"query, no credential", "GET", "/v1/conversations?account=" + acct, nil, ""},
		{"the bearer token itself", "GET", "/v1/conversations", map[string]string{"Authorization": "Bearer " + acct + acct}, ""},
		{"a header, bad credential", "GET", "/v1/conversations", map[string]string{"Authorization": "Bearer x", "X-Account": acct}, ""},
		{"a body that names an identity", "POST", "/v1/conversations", map[string]string{"Authorization": good, "Content-Type": "application/json"}, `{"tenant_id":"` + acct + `"}`},
		{"a body of the wrong type", "POST", "/v1/conversations", map[string]string{"Authorization": good, "Content-Type": "text/plain"}, "account " + acct},
		{"a body that is not JSON", "POST", "/v1/conversations", map[string]string{"Authorization": good, "Content-Type": "application/json"}, "account " + acct},
		{"a second document", "POST", "/v1/conversations", map[string]string{"Authorization": good, "Content-Type": "application/json"}, `{"a":1} {"account":"` + acct + `"}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := *calls
			req := httptest.NewRequest(c.method, c.target, strings.NewReader(c.body))
			for k, v := range c.header {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			out := rec.Body.String()
			for name, vals := range rec.Header() {
				out += " " + name + ":" + strings.Join(vals, ",")
			}
			if strings.Contains(out, acct) || digitRun.MatchString(typedID.ReplaceAllString(out, "ID")) {
				t.Errorf("the number reached the response (status %d): %s", rec.Code, out)
			}
			if rec.Code < http.StatusBadRequest && *calls == before {
				t.Errorf("status %d with no handler call: neither a refusal nor a success", rec.Code)
			}
		})
	}
}
