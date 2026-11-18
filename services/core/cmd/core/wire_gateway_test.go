package main

// EDG-01 (REQ-API-001, REQ-INP-002; INV-12, INV-02, AGENT-10, AGENT-13, AGENT-16): buildGateway makes the Input Gateway from file-mounted settings with a recorder and an
// index PER TENANT: a tenant's login appends and reads only that tenant's events, a tenant with no login is an error and never another tenant's, and the tenant is the session's,
// never the body's. /readyz names the gateway while it is not wired. Pools connect lazily and nothing here dials. DSNs, keys and tenants are SYNTHETIC. Tests that need the
// Record itself (RLS, the core-edge login) are the inputgateway package's and run in CI against PostgreSQL.

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/edge/edgetest"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/inputgateway"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

const (
	tenA = "ten_aa"
	tenB = "ten_bb"
)

type gwDirs struct{ edgeDSN, keys, readDSN, hash string }

// newGwDirs writes the synthetic settings of the gateway for the given tenants: a core-edge login and a record_read login per tenant, one core-edge key and a hash key.
func newGwDirs(t *testing.T, tenants ...string) gwDirs {
	t.Helper()
	d := gwDirs{edgeDSN: t.TempDir(), keys: t.TempDir(), readDSN: t.TempDir(), hash: filepath.Join(t.TempDir(), "hash.key")}
	for _, tn := range tenants {
		if err := os.WriteFile(filepath.Join(d.edgeDSN, tn), []byte(synthDSN("edge_"+tn)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d.readDSN, tn), []byte(synthDSN("read_"+tn)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeKey(t, d.keys, "core-edge.ed25519.1.key", aSeed, 0o400)
	if err := os.WriteFile(d.hash, []byte(strings.Repeat("h", 40)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return d
}

func (d gwDirs) env() envMap {
	return envMap{EnvEdgeDSNDir: d.edgeDSN, EnvEdgeKeyDir: d.keys, EnvEdgeKeyMaxActive: "2", EnvInputHashKeyFile: d.hash, EnvInputWindow: "24h", EnvInputRecordTimeout: "5s",
		edge.EnvMaxBody: "16384", EnvRecordReadDSNDir: d.readDSN}
}

func osEnv(e envMap) Env { return Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir} }

func noStart(context.Context, inputgateway.Turn) error { return nil }

func buildGW(t *testing.T, e envMap, records reconstruct.TenantDBs, start func(context.Context, inputgateway.Turn) error) (*inputgateway.Gateway, func(), error) {
	t.Helper()
	clk := clock.NewManual(apiEpoch)
	return buildGateway(t.Context(), osEnv(e), clk, ids.New(clk, nil), records, start)
}

func recordsOf(t *testing.T, e envMap) reconstruct.TenantDBs {
	t.Helper()
	r, closer, err := buildRecordReader(t.Context(), osEnv(e))
	if err != nil || r == nil {
		t.Fatalf("record reader: %v, %v", r, err)
	}
	t.Cleanup(closer)
	return r
}

// AGENT-16: with none of the settings there is no gateway and no error; /readyz then names it.
func TestWithNoGatewaySettingsThereIsNoGatewayAndNoError(t *testing.T) {
	g, closer, err := buildGW(t, envMap{}, nil, noStart)
	if err != nil || g != nil || closer == nil {
		t.Fatalf("got %v, closer %v, error %v; want no gateway, a callable closer and no error", g, closer != nil, err)
	}
	closer()
}

// A full, valid configuration builds the gateway, and the closer can be called twice.
func TestAFullConfigurationBuildsTheGateway(t *testing.T) {
	d := newGwDirs(t, tenA, tenB)
	e := d.env()
	g, closer, err := buildGW(t, e, recordsOf(t, e), noStart)
	if err != nil || g == nil {
		t.Fatalf("got %v, %v", g, err)
	}
	closer()
	closer()
}

// All settings or none: each missing one is an error that names it, with no gateway.
func TestAPartialConfigurationNamesTheMissingSetting(t *testing.T) {
	d := newGwDirs(t, tenA)
	for _, name := range []string{EnvEdgeDSNDir, EnvEdgeKeyDir, EnvEdgeKeyMaxActive, EnvInputHashKeyFile, EnvInputWindow, EnvInputRecordTimeout, edge.EnvMaxBody} {
		e := d.env()
		delete(e, name)
		g, _, err := buildGW(t, e, recordsOf(t, d.env()), noStart)
		if err == nil || g != nil || !strings.Contains(err.Error(), name) {
			t.Errorf("without %s: got %v, %v; want an error that names it and no gateway", name, g, err)
		}
	}
}

// Nothing is defaulted or guessed: a value that cannot be applied is an error that names its setting, and no message carries the hash key.
func TestASettingThatCannotBeAppliedIsAnErrorNamingIt(t *testing.T) {
	d := newGwDirs(t, tenA)
	short := filepath.Join(t.TempDir(), "short.key")
	if err := os.WriteFile(short, []byte("tooshort\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]envMap{
		EnvInputWindow:        {EnvInputWindow: "soon"},
		EnvInputRecordTimeout: {EnvInputRecordTimeout: "0s"},
		EnvEdgeKeyMaxActive:   {EnvEdgeKeyMaxActive: "0"},
		EnvInputHashKeyFile:   {EnvInputHashKeyFile: short},
		edge.EnvMaxBody:       {edge.EnvMaxBody: "-1"},
	}
	for name, change := range cases {
		e := d.env()
		for k, v := range change {
			e[k] = v
		}
		g, _, err := buildGW(t, e, recordsOf(t, d.env()), noStart)
		if err == nil || g != nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: got %v, %v; want an error that names it", name, g, err)
		}
		if err != nil && strings.Contains(err.Error(), strings.Repeat("h", 8)) {
			t.Errorf("%s: the error carries key material", name)
		}
	}
}

// The gateway needs the Record reader and the orchestrator seam: either missing is an error, never a gateway that cannot work.
func TestAGatewayWithoutTheRecordReaderOrTheSeamIsRefused(t *testing.T) {
	d := newGwDirs(t, tenA)
	e := d.env()
	if g, _, err := buildGW(t, e, nil, noStart); err == nil || g != nil || !strings.Contains(err.Error(), EnvRecordReadDSNDir) {
		t.Errorf("no Record reader: got %v, %v", g, err)
	}
	if g, _, err := buildGW(t, e, recordsOf(t, e), nil); err == nil || g != nil {
		t.Errorf("no seam: got %v, %v", g, err)
	}
}

// INV-12: a tenant with a core-edge login and no record_read login is an error naming the tenant (its inputs could not be looked up), and a core-edge key ring that cannot load is
// an error too.
func TestATenantWithoutAReadLoginAndAMissingKeyAreRefused(t *testing.T) {
	d := newGwDirs(t, tenA)
	if err := os.WriteFile(filepath.Join(d.edgeDSN, tenB), []byte(synthDSN("edge_"+tenB)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := d.env()
	if g, _, err := buildGW(t, e, recordsOf(t, e), noStart); err == nil || g != nil || !strings.Contains(err.Error(), tenB) {
		t.Errorf("tenant with no read login: got %v, %v; want an error naming %s", g, err, tenB)
	}
	d2 := newGwDirs(t, tenA)
	e2 := d2.env()
	e2[EnvEdgeKeyDir] = t.TempDir() // no key in it
	if g, _, err := buildGW(t, e2, recordsOf(t, d2.env()), noStart); err == nil || g != nil || !strings.Contains(err.Error(), EnvEdgeKeyDir) {
		t.Errorf("no signing key: got %v, %v", g, err)
	}
}

// fakeRecord is one tenant's recorder and index: it remembers whose identities reached it.
type fakeRecord struct {
	mu   sync.Mutex
	seen []string
	err  error
}

func (f *fakeRecord) note(id inputgateway.Identity) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, id.TenantID)
	return f.err
}
func (f *fakeRecord) Received(_ context.Context, id inputgateway.Identity, _ string, _ inputgateway.Received) (inputgateway.Ref, error) {
	return inputgateway.Ref{EventID: "ev_x", Seq: 1}, f.note(id)
}
func (f *fakeRecord) Deduplicated(_ context.Context, id inputgateway.Identity, _, _ string, _ inputgateway.Ref) error {
	return f.note(id)
}
func (f *fakeRecord) Reused(_ context.Context, id inputgateway.Identity, _, _, _ string, _ inputgateway.Ref) error {
	return f.note(id)
}
func (f *fakeRecord) Find(_ context.Context, id inputgateway.Identity, _ string) (inputgateway.Prior, bool, error) {
	return inputgateway.Prior{}, false, f.note(id)
}

func ident(tenant string) inputgateway.Identity {
	return inputgateway.Identity{TenantID: tenant, PrincipalID: "usr_1", PrincipalKind: "customer", SessionID: "ses_" + strings.Repeat("a", 32)}
}

// INV-12, REQ-SEC-004: every call goes to the identity's own tenant's recorder and index and to no other; a tenant with none is an error, with nothing asked of any other.
func TestEveryCallGoesToTheIdentitysOwnTenantAndNoOther(t *testing.T) {
	a, b := &fakeRecord{}, &fakeRecord{}
	rec := tenantRecorder{by: map[string]inputgateway.Recorder{tenA: a, tenB: b}}
	idx := tenantIndex{by: map[string]inputgateway.Index{tenA: a, tenB: b}}
	ctx := t.Context()
	if _, err := rec.Received(ctx, ident(tenA), "corr_x", inputgateway.Received{}); err != nil {
		t.Fatal(err)
	}
	if err := rec.Deduplicated(ctx, ident(tenA), "corr_x", "r", inputgateway.Ref{}); err != nil {
		t.Fatal(err)
	}
	if err := rec.Reused(ctx, ident(tenA), "corr_x", "r", "h", inputgateway.Ref{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := idx.Find(ctx, ident(tenA), "r"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := idx.Find(ctx, ident(tenB), "r"); err != nil {
		t.Fatal(err)
	}
	if len(a.seen) != 4 || len(b.seen) != 1 {
		t.Fatalf("tenant A saw %v, tenant B saw %v; want 4 and 1", a.seen, b.seen)
	}
	for _, s := range a.seen {
		if s != tenA {
			t.Errorf("tenant A's recorder was handed %s", s)
		}
	}
	for _, other := range []string{"ten_cc", "", "tnt_aa", "ten_aa/../ten_bb"} {
		if _, err := rec.Received(ctx, ident(other), "corr_x", inputgateway.Received{}); !errors.Is(err, ErrNoGatewayForTenant) {
			t.Errorf("recorder for %q: %v; want ErrNoGatewayForTenant", other, err)
		}
		if err := rec.Deduplicated(ctx, ident(other), "c", "r", inputgateway.Ref{}); !errors.Is(err, ErrNoGatewayForTenant) {
			t.Errorf("recorder (dedup) for %q: %v", other, err)
		}
		if err := rec.Reused(ctx, ident(other), "c", "r", "h", inputgateway.Ref{}); !errors.Is(err, ErrNoGatewayForTenant) {
			t.Errorf("recorder (reuse) for %q: %v", other, err)
		}
		if _, _, err := idx.Find(ctx, ident(other), "r"); !errors.Is(err, ErrNoGatewayForTenant) {
			t.Errorf("index for %q: %v; want ErrNoGatewayForTenant", other, err)
		}
	}
	if len(a.seen) != 4 || len(b.seen) != 1 {
		t.Errorf("a refused tenant reached a recorder: A %v, B %v", a.seen, b.seen)
	}
}

// Fail closed: a recorder's own error passes through (the gateway answers 503), it is not swallowed.
func TestARecorderErrorIsNotSwallowed(t *testing.T) {
	boom := errors.New("synthetic: append failed")
	rec := tenantRecorder{by: map[string]inputgateway.Recorder{tenA: &fakeRecord{err: boom}}}
	if _, err := rec.Received(t.Context(), ident(tenA), "corr_x", inputgateway.Received{}); !errors.Is(err, boom) {
		t.Errorf("got %v; want the recorder's error", err)
	}
}

// /readyz names the gateway while it is not wired (fail closed, no quiet 200), keeps the inner reason first, and is ready only with the gateway and the inner check ready.
func TestReadinessNamesTheGatewayUntilItIsWired(t *testing.T) {
	ready := func() (bool, string) { return true, "" }
	ok, reason := withGatewayReadiness(ready, Components{})()
	if ok || !strings.Contains(reason, "gateway") {
		t.Errorf("no gateway: ready %v, reason %q; want not ready and a reason that names gateway", ok, reason)
	}
	g := newGateway(t, &memGateway{prior: map[string]inputgateway.Prior{}})
	if ok, reason := withGatewayReadiness(ready, Components{Gateway: g})(); !ok || reason != "" {
		t.Errorf("wired: ready %v, reason %q; want ready", ok, reason)
	}
	if ok, reason := withGatewayReadiness(notReady, Components{Gateway: g})(); ok || reason != "dependencies are not configured" {
		t.Errorf("inner not ready: ready %v, reason %q; want the inner reason", ok, reason)
	}
	mux := newMux(withGatewayReadiness(ready, Components{}), nil)
	rec := call(mux, "/readyz", "")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "gateway") {
		t.Errorf("/readyz = %d %q; want 503 naming the gateway", rec.Code, rec.Body.String())
	}
}

// withGateway mounts POST /v1/conversations behind the session (INV-12): the recorded tenant is the session's, a tenant named in the body is refused before anything is recorded,
// another tenant's session reaches only its own recorder, and nothing is mounted with no gateway.
func TestTheMountedRouteRecordsOnlyAsTheSessionsTenant(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	a, b := &fakeRecord{}, &fakeRecord{}
	var turns []string
	var mu sync.Mutex
	start := func(_ context.Context, tn inputgateway.Turn) error {
		mu.Lock()
		defer mu.Unlock()
		turns = append(turns, tn.TenantID)
		return nil
	}
	clk := clock.NewManual(apiEpoch)
	g, err := inputgateway.New(inputgateway.Config{Recorder: tenantRecorder{by: map[string]inputgateway.Recorder{tenA: a, tenB: b}}, Index: tenantIndex{by: map[string]inputgateway.Index{tenA: a, tenB: b}},
		Start: start, IDs: fixedIDs{ids.New(clk, nil)}, MaxBody: 16384, HashKey: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	e := sessionEnv(t, iss)
	e[edge.EnvMaxBody] = "16384"
	e[edge.EnvTenants] = tenA + "," + tenB
	h, err := buildAPI(withGateway(Components{}, g), Env{Getenv: e.get, ReadFile: os.ReadFile}, clk, ids.New(clk, nil))
	if err != nil || h == nil {
		t.Fatalf("buildAPI: %v, %v", h, err)
	}
	if rec := post(h, bearer(iss, session.RoleCustomer, tenB), startBody(aRequestID, "where is my money")); rec.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if len(a.seen) != 0 || len(b.seen) != 2 || len(turns) != 1 || turns[0] != tenB { // one Find and one Received, both as B
		t.Errorf("A saw %v, B saw %v, turns %v; want only tenant B", a.seen, b.seen, turns)
	}
	named := `{"request_id":"` + aRequestID + `","channel":"text","text":"hi","tenant_id":"` + tenA + `"}`
	if rec := post(h, bearer(iss, session.RoleCustomer, tenB), named); rec.Code != http.StatusBadRequest {
		t.Errorf("a tenant in the body: %d, want 400", rec.Code)
	}
	if len(a.seen) != 0 || len(b.seen) != 2 {
		t.Errorf("a refused body reached a recorder: A %v, B %v", a.seen, b.seen)
	}
	if rec := post(h, bearer(iss, session.RoleOps, tenB), startBody(aRequestID, "x")); rec.Code != http.StatusNotFound {
		t.Errorf("an ops session: %d, want 404", rec.Code)
	}
	if rec := post(h, "", startBody(aRequestID, "x")); rec.Code != http.StatusUnauthorized {
		t.Errorf("no session: %d, want 401", rec.Code)
	}
	if routes := apiRoutes(withGateway(Components{}, nil), ids.New(clk, nil)); len(routes) != 0 {
		t.Errorf("no gateway mounted %d routes", len(routes))
	}
}

// Defence in depth: a map entry whose key is not a tenant ID of common.v1 is never served, even for an identity that carries exactly that key.
func TestAnEntryThatIsNotATenantIDIsNeverServed(t *testing.T) {
	f := &fakeRecord{}
	rec := tenantRecorder{by: map[string]inputgateway.Recorder{"tnt_xx": f}}
	idx := tenantIndex{by: map[string]inputgateway.Index{"tnt_xx": f}}
	if _, err := rec.Received(t.Context(), ident("tnt_xx"), "corr_x", inputgateway.Received{}); !errors.Is(err, ErrNoGatewayForTenant) {
		t.Errorf("recorder: %v", err)
	}
	if _, _, err := idx.Find(t.Context(), ident("tnt_xx"), "r"); !errors.Is(err, ErrNoGatewayForTenant) {
		t.Errorf("index: %v", err)
	}
	if len(f.seen) != 0 {
		t.Errorf("reached %v", f.seen)
	}
}
