package main

// EDG-01 slice 3c, part 2: main wires the protected /v1 API from the environment (REQ-API-004, REQ-AUD-003, REQ-SEC-004, INV-12, AGENT-16). With none of the session settings the
// core serves health only; with some it refuses to start; with all of them every /v1 call passes the verified session and the role gate (deny by default) before any
// handler, and the reconstruction route is mounted for ops and audit only, from the token's tenant (never a query, header or path). The Record reader (one pool per tenant) is a
// component another task configures: with none, the route is not mounted and the readiness check says so. Keys and tokens are SYNTHETIC, made per test (AGENT-25).

import (
	"bytes"
	"errors"
	"log/slog"
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
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

var apiEpoch = time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)

const (
	apiKid = "idp-main-k7"
	apiAud = "noema-main-aud"
)

type envMap map[string]string

func (e envMap) get(k string) string { return e[k] }

// failingDBs is a Record reader that cannot be reached: the handler answers 500 after it has authenticated and authorised the caller, which is what the test needs to
// see. It remembers the tenant it was asked for.
type failingDBs struct{ asked []string }

func (f *failingDBs) For(tenant string) (reconstruct.TxBeginner, error) {
	f.asked = append(f.asked, tenant)
	return nil, errors.New("no pool in this test")
}

func discardLog() *slog.Logger { return slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)) }

func sessionEnv(t *testing.T, iss *edgetest.Issuer) envMap {
	t.Helper()
	dir := t.TempDir()
	key, lim := filepath.Join(dir, "idp.pub"), filepath.Join(dir, "limits.json")
	if err := os.WriteFile(key, iss.PublicPEM(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lim, []byte(`{"schema_version":"1.0","pack_id":"fin","label":"SYNTHETIC","clock_skew_s":60}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return envMap{edge.EnvKeyFile: key, edge.EnvKid: apiKid, edge.EnvAudience: apiAud, edge.EnvTenants: edgetest.Tenant + ",tnt_other", edge.EnvLimitsFile: lim, edge.EnvMaxBody: "1024"}
}

func api(t *testing.T, e envMap, c Components) (http.Handler, error) {
	t.Helper()
	clk := clock.NewManual(apiEpoch)
	return buildAPI(c, Env{Getenv: e.get, ReadFile: os.ReadFile}, clk, ids.New(clk, nil))
}

func bearer(iss *edgetest.Issuer, role session.Role, tenant string) string {
	c := edgetest.Good(apiEpoch, role)
	c.Aud, c.Tenant = apiAud, tenant
	return "Bearer " + iss.Token(apiKid, c)
}

func call(h http.Handler, path, authz string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// AGENT-16: no session settings is no API: the core serves health only and is not ready for traffic.
func TestWithNoSessionSettingsThereIsNoAPI(t *testing.T) {
	h, err := api(t, envMap{}, Components{})
	if err != nil || h != nil {
		t.Fatalf("handler %v, error %v", h, err)
	}
	mux := newMux(notReady, h)
	if rec := call(mux, "/healthz", ""); rec.Code != http.StatusOK {
		t.Errorf("/healthz = %d", rec.Code)
	}
	if rec := call(mux, "/v1/reconstructions/corr_ABC123", ""); rec.Code != http.StatusNotFound {
		t.Errorf("/v1 with no API = %d, want 404", rec.Code)
	}
}

// Fail closed: some settings, or settings that are wrong, are an error and no handler; so is a Record reader without any session settings (it would be unreachable).
func TestPartialSettingsOrARecordReaderWithoutASessionRefuseToStart(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	e := sessionEnv(t, iss)
	delete(e, edge.EnvAudience)
	if h, err := api(t, e, Components{}); err == nil || h != nil {
		t.Errorf("partial settings: handler %v, error %v", h, err)
	}
	if h, err := api(t, envMap{}, Components{Records: &failingDBs{}, AccessLog: discardLog()}); err == nil || h != nil {
		t.Errorf("a Record reader with no session settings: handler %v, error %v", h, err)
	}
}

// REQ-AUD-003, REQ-API-004: ops and audit reach the reconstruction handler through the real session check and role gate; every other caller is refused before it, and the
// tenant it asks the Record reader for is the token's.
func TestTheReconstructionRouteIsMountedForOpsAndAuditBehindTheSessionAndTheRoleGate(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	dbs := &failingDBs{}
	h, err := api(t, sessionEnv(t, iss), Components{Records: dbs, AccessLog: discardLog()})
	if err != nil || h == nil {
		t.Fatalf("handler %v, error %v", h, err)
	}
	path := "/v1/reconstructions/corr_ABC123"
	for _, role := range []session.Role{session.RoleOps, session.RoleAudit} {
		rec := call(h, path+"?tenant_id=tnt_x", bearer(iss, role, "tnt_other"))
		if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "INTERNAL_SAFETY_BLOCK") {
			t.Errorf("%s: %d %s; want the handler's answer", role, rec.Code, rec.Body.String())
		}
	}
	if len(dbs.asked) != 2 || dbs.asked[0] != "tnt_other" || dbs.asked[1] != "tnt_other" {
		t.Errorf("the Record reader was asked for %v; want the token's tenant twice, not the query's", dbs.asked)
	}
	before := len(dbs.asked)
	for name, tc := range map[string]struct {
		authz  string
		status int
	}{
		"customer":            {bearer(iss, session.RoleCustomer, edgetest.Tenant), http.StatusNotFound},
		"approver":            {bearer(iss, session.RoleApprover, edgetest.Tenant), http.StatusNotFound},
		"no session":          {"", http.StatusUnauthorized},
		"a bad token":         {"Bearer junk", http.StatusUnauthorized},
		"a tenant not served": {bearer(iss, session.RoleOps, "tnt_unlisted"), http.StatusUnauthorized},
	} {
		if rec := call(h, path, tc.authz); rec.Code != tc.status {
			t.Errorf("%s: %d %s; want %d", name, rec.Code, rec.Body.String(), tc.status)
		}
	}
	if len(dbs.asked) != before {
		t.Errorf("a refused caller reached the Record reader: %v", dbs.asked[before:])
	}
	if rec := call(h, "/v1/nothing", bearer(iss, session.RoleOps, edgetest.Tenant)); rec.Code != http.StatusNotFound {
		t.Errorf("an unregistered route = %d, want 404 (deny by default)", rec.Code)
	}
}

// With no Record reader the route is not mounted at all: an ops session finds nothing, and the readiness check says what is missing.
func TestWithoutARecordReaderTheRouteIsNotMountedAndReadinessSaysSo(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	h, err := api(t, sessionEnv(t, iss), Components{})
	if err != nil || h == nil {
		t.Fatalf("handler %v, error %v", h, err)
	}
	if rec := call(h, "/v1/reconstructions/corr_ABC123", bearer(iss, session.RoleOps, edgetest.Tenant)); rec.Code != http.StatusNotFound {
		t.Errorf("unmounted route = %d, want 404", rec.Code)
	}
	ok, reason := composeReadiness(full(), nil)()
	if ok || !strings.Contains(reason, "edge chain") {
		t.Errorf("ready %v, reason %q; want not ready and the edge chain named", ok, reason)
	}
	c := full()
	c.Records = nil
	ok, reason = composeReadiness(c, h)()
	if ok || !strings.Contains(reason, "record reader") {
		t.Errorf("ready %v, reason %q; want not ready and the record reader named", ok, reason)
	}
	c.Records = &failingDBs{}
	if ok, reason = composeReadiness(c, h)(); !ok {
		t.Errorf("a full composition is not ready: %q", reason)
	}
}

// The health routes stay public and outside /v1; /v1 is served only through the protected handler.
func TestHealthStaysPublicAndV1GoesThroughTheProtectedHandler(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	h, err := api(t, sessionEnv(t, iss), Components{Records: &failingDBs{}, AccessLog: discardLog()})
	if err != nil {
		t.Fatal(err)
	}
	mux := newMux(notReady, h)
	if rec := call(mux, "/healthz", ""); rec.Code != http.StatusOK {
		t.Errorf("/healthz = %d", rec.Code)
	}
	if rec := call(mux, "/readyz", ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("/readyz = %d", rec.Code)
	}
	if rec := call(mux, "/v1/reconstructions/corr_ABC123", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("/v1 without a session = %d, want 401", rec.Code)
	}
}

// The route table is the one place that says who may call what: the reconstruction route lists exactly ops and audit (REQ-AUD-003, ruling A20), so a customer or an
// approver is refused by the edge gate before the handler runs, not only by the handler's own check.
func TestTheRouteTableListsExactlyOpsAndAuditForTheReconstruction(t *testing.T) {
	clk := clock.NewManual(apiEpoch)
	routes := apiRoutes(Components{Records: &failingDBs{}, AccessLog: discardLog()}, ids.New(clk, nil))
	if len(routes) != 1 || routes[0].Pattern != reconstruct.Route {
		t.Fatalf("routes %+v", routes)
	}
	got := map[session.Role]bool{}
	for _, r := range routes[0].Roles {
		got[r] = true
	}
	if len(got) != 2 || !got[session.RoleOps] || !got[session.RoleAudit] {
		t.Fatalf("roles %v; want exactly ops and audit", routes[0].Roles)
	}
	if none := apiRoutes(Components{}, ids.New(clk, nil)); len(none) != 0 {
		t.Fatalf("a route without its component: %+v", none)
	}
}

// REQ-AUD-003: a Record read must be logged. A Record reader without an access logger is a startup error, never a route that serves silently.
func TestARecordReaderWithoutAnAccessLoggerRefusesToStart(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	if h, err := api(t, sessionEnv(t, iss), Components{Records: &failingDBs{}}); err == nil || h != nil {
		t.Errorf("a Record reader with no access logger: handler %v, error %v", h, err)
	}
}

// REQ-AUD-003: the call that reaches the reconstruction handler writes one RECORD_ACCESSED line carrying the token's tenant and principal and an outcome code, and nothing of
// the Record. (The SERVED outcome is asserted with a real Record in the reconstruct package's own tests.)
func TestACallThatReachesTheReconstructionHandlerIsLoggedAsRecordAccessed(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	var buf bytes.Buffer
	h, err := api(t, sessionEnv(t, iss), Components{Records: &failingDBs{}, AccessLog: slog.New(slog.NewJSONHandler(&buf, nil))})
	if err != nil || h == nil {
		t.Fatalf("handler %v, error %v", h, err)
	}
	if rec := call(h, "/v1/reconstructions/corr_ABC123", bearer(iss, session.RoleAudit, "tnt_other")); rec.Code != http.StatusInternalServerError {
		t.Fatalf("answer %d %s", rec.Code, rec.Body.String())
	}
	line := buf.String()
	for _, want := range []string{`"msg":"RECORD_ACCESSED"`, `"tenant_id":"tnt_other"`, `"outcome":"INTERNAL_SAFETY_BLOCK"`} {
		if !strings.Contains(line, want) {
			t.Errorf("access log %q lacks %s", line, want)
		}
	}
	if n := strings.Count(line, "RECORD_ACCESSED"); n != 1 {
		t.Errorf("%d RECORD_ACCESSED lines, want 1", n)
	}
}
