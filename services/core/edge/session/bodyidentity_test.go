package session_test

// Ruling A1 (D7): no identity from a request body (INV-12, AGENT-10). A body that names a principal, a tenant, a role or an issuer is refused, and
// the handler never runs, so there is no path on which the body could be used. Only JSON bodies are accepted (deny by default); a body that
// cannot be read in full within the limit is refused too.

import (
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
)

const bodyLimit = 256

func guarded(t *testing.T, k keys) (http.Handler, *int, *string) {
	t.Helper()
	v, clk := verifier(t, k, epoch)
	calls, got := 0, ""
	idg := ids.New(clk, nil)
	guard, err := session.RefuseBodyIdentity(idg, bodyLimit)
	if err != nil {
		t.Fatal(err)
	}
	h := session.Middleware(v, idg)(guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		w.WriteHeader(http.StatusNoContent)
	})))
	return h, &calls, &got
}

func post(h http.Handler, authz, contentType, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/conversations", strings.NewReader(body))
	req.Header.Set("Authorization", authz)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestABodyThatNamesAnIdentityIsRefusedAndTheHandlerNeverRuns(t *testing.T) {
	k := newKeys(t)
	h, calls, _ := guarded(t, k)
	tok := "Bearer " + mint(t, k.priv, goodHeader(), goodClaims(epoch))
	for name, body := range map[string]string{
		"principal_id":                 `{"principal_id":"op_9","message":"hi"}`,
		"tenant_id":                    `{"tenant_id":"tnt_b"}`,
		"sub":                          `{"sub":"op_9"}`,
		"roles":                        `{"roles":["ops"]}`,
		"role":                         `{"role":"audit"}`,
		"iss":                          `{"iss":"noema-poc-idp"}`,
		"user_id":                      `{"user_id":"u1"}`,
		"actor":                        `{"actor":"operator:op_9"}`,
		"another case":                 `{"Tenant_ID":"tnt_b"}`,
		"an escaped name":              `{"tenant\u005fid":"tnt_b"}`,
		"the name twice":               `{"tenant_id":"a","tenant_id":"b"}`,
		"long s folds to s (sub)":      "{\"\u017fub\":\"usr_other\"}",
		"long s folds to s (iss)":      "{\"i\u017f\u017f\":\"evil\"}",
		"kelvin sign folds to k":       "{\"to\u212aen\":\"x\"}",
		"nested in an object":          `{"a":{"b":{"tenant_id":"tnt_b"}}}`,
		"nested in an array":           `{"items":[{"ok":1},{"principal_id":"op_9"}]}`,
		"after a large harmless field": `{"note":"` + strings.Repeat("x", 100) + `","tenant_id":"tnt_b"}`,
	} {
		before := *calls
		rec := post(h, tok, "application/json", body)
		if rec.Code != http.StatusBadRequest || envelope(t, rec).Code != contracts.ErrorsV2ErrorCodeINPUTINVALID || *calls != before {
			t.Errorf("%s: %d %s, handler calls %d->%d; want 400 INPUT_INVALID and no handler call", name, rec.Code, rec.Body.String(), before, *calls)
		}
		if strings.Contains(rec.Body.String(), "op_9") || strings.Contains(rec.Body.String(), "tnt_b") {
			t.Errorf("%s: the refusal echoes the identity: %s", name, rec.Body.String())
		}
	}
}

func TestAnOrdinaryBodyPassesUnchangedAndTheWordInAValueIsNotAnIdentity(t *testing.T) {
	k := newKeys(t)
	h, calls, got := guarded(t, k)
	tok := "Bearer " + mint(t, k.priv, goodHeader(), goodClaims(epoch))
	for _, body := range []string{``, `{}`, `{"message":"my tenant_id is wrong","amount":5}`, `{"items":[{"id":1},{"id":2}]}`, `[1,2,3]`} {
		before := *calls
		ct := "application/json; charset=utf-8"
		rec := post(h, tok, ct, body)
		if rec.Code != http.StatusNoContent || *calls != before+1 || *got != body {
			t.Errorf("%q: %d, calls %d->%d, handler saw %q", body, rec.Code, before, *calls, *got)
		}
	}
}

func TestADenyByDefaultBodyIsRefused(t *testing.T) {
	k := newKeys(t)
	h, calls, _ := guarded(t, k)
	tok := "Bearer " + mint(t, k.priv, goodHeader(), goodClaims(epoch))
	for name, c := range map[string]struct{ ct, body string }{
		"not JSON":                         {"application/json", `not json`},
		"truncated JSON":                   {"application/json", `{"a":`},
		"two documents":                    {"application/json", `{} {"tenant_id":"x"}`},
		"form encoded":                     {"application/x-www-form-urlencoded", `tenant_id=tnt_b`},
		"a JSON body labelled text/plain":  {"text/plain", `{"a":1}`},
		"plain text":                       {"text/plain", `tenant_id=tnt_b`},
		"no content type with a body":      {"", `{"a":1}`},
		"over the limit":                   {"application/json", `{"note":"` + strings.Repeat("x", bodyLimit) + `"}`},
		"valid JSON padded past the limit": {"application/json", `{}` + strings.Repeat(" ", bodyLimit)},
	} {
		before := *calls
		rec := post(h, tok, c.ct, c.body)
		if rec.Code != http.StatusBadRequest || *calls != before {
			t.Errorf("%s: %d %s, handler calls %d->%d; want 400 and no handler call", name, rec.Code, rec.Body.String(), before, *calls)
		}
	}
}

func TestTheBodyGuardCannotBeReachedWithoutASession(t *testing.T) {
	k := newKeys(t)
	h, calls, _ := guarded(t, k)
	rec := post(h, "", "application/json", `{"tenant_id":"tnt_b"}`)
	if rec.Code != http.StatusUnauthorized || *calls != 0 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

// Codex P1 on noema#198 (INV-12, A1 deny by default): the limit is checked when the guard is built. A limit of math.MaxInt64 made the read bound
// maxBytes+1 overflow to a negative number, so nothing was read, the body looked empty and the handler ran with the identity unscanned.
func TestAGuardWithAnUnusableBodyLimitIsNotBuilt(t *testing.T) {
	idg := ids.New(clock.NewManual(epoch), nil)
	for name, limit := range map[string]int64{"zero": 0, "negative": -1, "min int64": math.MinInt64, "max int64": math.MaxInt64} {
		guard, err := session.RefuseBodyIdentity(idg, limit)
		if !errors.Is(err, session.ErrBodyLimit) || guard != nil {
			t.Errorf("%s (%d): got guard %v, error %v; want no guard and ErrBodyLimit", name, limit, guard != nil, err)
		}
	}
	if guard, err := session.RefuseBodyIdentity(idg, 1); err != nil || guard == nil {
		t.Errorf("a limit of 1 byte: guard %v, error %v; want a guard", guard != nil, err)
	}
	if guard, err := session.RefuseBodyIdentity(idg, math.MaxInt64-1); err != nil || guard == nil {
		t.Errorf("the largest limit that does not overflow: guard %v, error %v; want a guard", guard != nil, err)
	}
}

// With the largest limit that can be built, a body that names an identity is still refused and the handler never runs.
func TestAHugeButValidLimitStillScansTheBody(t *testing.T) {
	k := newKeys(t)
	v, clk := verifier(t, k, epoch)
	idg := ids.New(clk, nil)
	guard, err := session.RefuseBodyIdentity(idg, math.MaxInt64-1)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	h := session.Middleware(v, idg)(guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(http.StatusNoContent) })))
	tok := "Bearer " + mint(t, k.priv, goodHeader(), goodClaims(epoch))
	rec := post(h, tok, "application/json", `{"tenant_id":"tnt_b"}`)
	if rec.Code != http.StatusBadRequest || calls != 0 {
		t.Fatalf("%d %s, handler calls %d; want 400 and no handler call", rec.Code, rec.Body.String(), calls)
	}
}
