package main

// EDG-01 slice 3b. REQ-API-004: the core process puts every /v1 call behind a verified session, the identity-in-body guard and the role gate;
// the health endpoints stay public. AGENT-16: no number or key lives in code, so the verifier's keys, audience, tenants, skew and the body
// limit come from configuration, and a core with some of it missing does not start (fail closed). A core with none of it serves health only
// and says it is not ready. Keys and tokens are SYNTHETIC, made per test (AGENT-25).

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/edge/edgetest"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
)

var wiringEpoch = time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)

// The kid and the audience of the configured core are not the helper's defaults, so a core that ignored its configuration would refuse these tokens.
const (
	wiredKid = "idp-wired-k9"
	wiredAud = "noema-wired-aud"
)

func wired(iss *edgetest.Issuer, role session.Role) string {
	c := edgetest.Good(wiringEpoch, role)
	c.Aud = wiredAud
	return iss.Token(wiredKid, c)
}

const limitsOK = `{"schema_version":"1.0","pack_id":"fintech","label":"SYNTHETIC","clock_skew_s":60}`

type env map[string]string

func (e env) get(k string) string { return e[k] }

func files(t *testing.T, iss *edgetest.Issuer, limits string) env {
	t.Helper()
	dir := t.TempDir()
	key, lim := filepath.Join(dir, "idp.pub"), filepath.Join(dir, "limits.json")
	if err := os.WriteFile(key, iss.PublicPEM(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lim, []byte(limits), 0o600); err != nil {
		t.Fatal(err)
	}
	return env{envKeyFile: key, envKid: wiredKid, envAudience: wiredAud, envTenants: edgetest.Tenant, envLimitsFile: lim, envMaxBody: "1024"}
}

func load(e env, clk clock.Clock) (http.Handler, error) {
	return loadEdge(e.get, os.ReadFile, clk, ids.New(clk, nil), nil)
}

// A core with none of the session configuration serves no /v1 at all.
func TestNoSessionConfigurationMountsNoAPI(t *testing.T) {
	h, err := load(env{}, clock.NewManual(wiringEpoch))
	if err != nil || h != nil {
		t.Fatalf("got handler %v, error %v", h, err)
	}
	rec := httptest.NewRecorder()
	newHandler(notReady, h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/cases", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/v1/cases without configuration = %d, want 404", rec.Code)
	}
	rec = httptest.NewRecorder()
	newHandler(notReady, h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz = %d, want 503", rec.Code)
	}
}

// Fail closed: any one setting missing, with the others given, is a refusal that names the variable and no value.
func TestAPartialConfigurationIsRefused(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	for _, name := range []string{envKeyFile, envKid, envAudience, envTenants, envLimitsFile, envMaxBody} {
		e := files(t, iss, limitsOK)
		delete(e, name)
		h, err := load(e, clock.NewManual(wiringEpoch))
		if err == nil || h != nil || !strings.Contains(err.Error(), name) {
			t.Errorf("without %s: handler %v, error %v", name, h, err)
		}
	}
}

// Fail closed: a bad key, limits document, tenant list, body limit or a skew that is not below the lifetime ceiling is a refusal.
func TestABadConfigurationIsRefused(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	bad := map[string]func(env){
		"key file missing":        func(e env) { e[envKeyFile] = filepath.Join(t.TempDir(), "nope") },
		"key file not a key":      func(e env) { _ = os.WriteFile(e[envKeyFile], []byte("hello"), 0o600) },
		"limits not the contract": func(e env) { _ = os.WriteFile(e[envLimitsFile], []byte(`{"clock_skew_s":60}`), 0o600) },
		"skew at the ceiling": func(e env) {
			_ = os.WriteFile(e[envLimitsFile], []byte(strings.Replace(limitsOK, "60", "900", 1)), 0o600)
		},
		"body limit not a number": func(e env) { e[envMaxBody] = "big" },
		"body limit zero":         func(e env) { e[envMaxBody] = "0" },
		"body limit negative":     func(e env) { e[envMaxBody] = "-5" },
		"body limit max int64":    func(e env) { e[envMaxBody] = "9223372036854775807" },
		"empty tenant in list":    func(e env) { e[envTenants] = "tnt_a,,tnt_b" },
	}
	for name, mutate := range bad {
		e := files(t, iss, limitsOK)
		mutate(e)
		if h, err := load(e, clock.NewManual(wiringEpoch)); err == nil || h != nil {
			t.Errorf("%s: accepted (handler %v, error %v)", name, h, err)
		} else if strings.Contains(err.Error(), "BEGIN") {
			t.Errorf("%s: the error repeats key material: %v", name, err)
		}
	}
}

// REQ-API-004, REQ-SEC-001: with the configuration, /v1 is behind the verified session, deny by default; health stays public.
func TestTheConfiguredCoreProtectsV1AndLeavesHealthPublic(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	clk := clock.NewManual(wiringEpoch)
	e := files(t, iss, limitsOK)
	e[envTenants] = "tnt_x," + edgetest.Tenant
	protected, err := load(e, clk)
	if err != nil || protected == nil {
		t.Fatalf("handler %v, error %v", protected, err)
	}
	h := newHandler(notReady, protected)
	call := func(path, bearer string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := call("/healthz", ""); got != http.StatusOK {
		t.Errorf("/healthz = %d", got)
	}
	if got := call("/v1/cases", ""); got != http.StatusUnauthorized {
		t.Errorf("/v1/cases without a session = %d", got)
	}
	if got := call("/v1/cases", "garbage"); got != http.StatusUnauthorized {
		t.Errorf("/v1/cases with a bad session = %d", got)
	}
	tok := wired(iss, session.RoleOps)
	if got := call("/v1/cases", tok); got != http.StatusNotFound {
		t.Errorf("/v1/cases with a valid session but no registered route = %d, want 404 (deny by default)", got)
	}
	other := edgetest.Good(wiringEpoch, session.RoleOps)
	other.Tenant, other.Aud = "tnt_unlisted", wiredAud
	if got := call("/v1/cases", iss.Token(wiredKid, other)); got != http.StatusUnauthorized {
		t.Errorf("a tenant the deployment does not serve = %d, want 401", got)
	}
	if got := call("/v1/cases", iss.Token(edgetest.Kid, edgetest.Good(wiringEpoch, session.RoleOps))); got != http.StatusUnauthorized {
		t.Errorf("a token for another audience and key id than the configured ones = %d, want 401", got)
	}
	if got := call("/v1/cases", iss.Token("idp-other", edgetest.Good(wiringEpoch, session.RoleOps))); got != http.StatusUnauthorized {
		t.Errorf("an unknown key id = %d, want 401", got)
	}
}

// AGENT-16, ruling A21: the leeway is the pack's clock_skew_s and nothing in code. A session 30 s past its expiry is accepted under a 60 s
// leeway and refused under none.
func TestTheLeewayIsTheLimitsDocumentsAndNothingElse(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	clk := clock.NewManual(wiringEpoch)
	tok := wired(iss, session.RoleOps)
	clk.Advance(5*time.Minute + 30*time.Second) // 30 s past exp
	for skew, want := range map[string]int{"60": http.StatusNotFound, "0": http.StatusUnauthorized} {
		e := files(t, iss, strings.Replace(limitsOK, `"clock_skew_s":60`, `"clock_skew_s":`+skew, 1))
		protected, err := load(e, clk)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "/v1/cases", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		newHandler(notReady, protected).ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("skew %s s: got %d, want %d", skew, rec.Code, want)
		}
	}
}

// AGENT-16: the body limit is the configured one. With a route registered and a limit of 1024, a 2 KiB body is refused and a small one passes.
func TestTheBodyLimitIsTheConfiguredOne(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	clk := clock.NewManual(wiringEpoch)
	e := files(t, iss, limitsOK)
	route := edge.Route{Pattern: "POST /v1/probe", Roles: []session.Role{session.RoleOps}, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })}
	protected, err := loadEdge(e.get, os.ReadFile, clk, ids.New(clk, nil), []edge.Route{route})
	if err != nil {
		t.Fatal(err)
	}
	post := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, "/v1/probe", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+wired(iss, session.RoleOps))
		rec := httptest.NewRecorder()
		newHandler(notReady, protected).ServeHTTP(rec, req)
		return rec.Code
	}
	if got := post(`{"a":1}`); got != http.StatusNoContent {
		t.Errorf("small body = %d", got)
	}
	if got := post(`{"a":"` + strings.Repeat("x", 2048) + `"}`); got != http.StatusBadRequest {
		t.Errorf("2 KiB body under a 1024-byte limit = %d, want 400", got)
	}
}
