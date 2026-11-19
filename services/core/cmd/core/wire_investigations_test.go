package main

// STM-01 / noema#601, ruling A243 (committed in docs as an interim record): the production adapter of the InvestigationReader port, over migration 0029 and the
// orchestrator's own per-tenant logins (the pools the Recorder already has; no new setting, AGENT-16), plugged into the components the process builds. These tests run on a real
// PostgreSQL with one orchestrator_rw login per tenant (row-level security with FORCE, migration 0025). An investigation is served only to the principal that started it, in
// its own tenant; every other case is the one not-found shape, and a store that fails is an error (503 in the route). REQ-STM-001, REQ-API-001, REQ-SEC-004, INV-12.
// Tokens, DSNs and keys are SYNTHETIC.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/edge/edgetest"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
)

// TestMain stops the PostgreSQL server the package's database tests start, so a run leaves no server and no data directory behind (internal/testpg).
func TestMain(m *testing.M) { testpg.Main(m) }

const (
	wireInvA1 = "inv_01ARZ3NDEKTSV4RRFFQ69G5FAA" // tenant A, started by prn_1, EXECUTING
	wireInvA2 = "inv_01ARZ3NDEKTSV4RRFFQ69G5FBB" // tenant A, started by prn_2, COMPLETED
	wireInvB1 = "inv_01ARZ3NDEKTSV4RRFFQ69G5FCC" // tenant B, started by prn_1, AWAITING_CONFIRMATION
	wireInvLg = "inv_01ARZ3NDEKTSV4RRFFQ69G5FDD" // tenant A, a row from before migration 0029: no principal
	wireInvNo = "inv_01ARZ3NDEKTSV4RRFFQ69G5FEE" // nowhere
)

type wireWorld struct {
	admin      *pgxpool.Pool
	rec, orch  map[string]*pgxpool.Pool // the logins of each tenant, which the DSN files are made from
	refCounter int
}

func newWireWorld(t *testing.T) *wireWorld {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(deadline(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	w := &wireWorld{admin: admin, rec: map[string]*pgxpool.Pool{}, orch: map[string]*pgxpool.Pool{}}
	for _, tenant := range []string{"ten_aa", "ten_bb"} {
		if _, err := admin.Exec(deadline(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Synthetic bank')`, tenant); err != nil {
			t.Fatal(err)
		}
		w.rec[tenant] = testpg.TenantLogin(t, admin, dsn, "record_read", tenant)
		w.orch[tenant] = testpg.TenantLogin(t, admin, dsn, "orchestrator_rw", tenant)
	}
	// A row that predates the CHECK of migration 0029 has no principal: the constraint is dropped here only so that such a row can be written to the test database.
	if _, err := admin.Exec(deadline(t), `ALTER TABLE investigation DROP CONSTRAINT investigation_principal_named`); err != nil {
		t.Fatal(err)
	}
	w.seed(t, "ten_aa", wireInvA1, "EXECUTING", "prn_1")
	w.seed(t, "ten_aa", wireInvA2, "COMPLETED", "prn_2")
	w.seed(t, "ten_bb", wireInvB1, "AWAITING_CONFIRMATION", "prn_1")
	w.seed(t, "ten_aa", wireInvLg, "RECEIVED", "")
	return w
}

// seed writes one investigation as the administrator: a new row is RECEIVED at version 0 and any other state is one move.
func (w *wireWorld) seed(t *testing.T, tenant, inv, state, principal string) {
	t.Helper()
	w.refCounter++
	ref := "recev_" + strings.Repeat("A", 4) + string(rune('a'+w.refCounter))
	if _, err := w.admin.Exec(deadline(t), `INSERT INTO investigation (tenant_id, investigation_id, input_received_ref, correlation_id, state, state_version, principal_ref)
		VALUES ($1, $2, $3, 'corr_AA11', 'RECEIVED', 0, $4)`, tenant, inv, ref, principal); err != nil {
		t.Fatal(err)
	}
	if state != "RECEIVED" {
		if _, err := w.admin.Exec(deadline(t), `UPDATE investigation SET state = $1, state_version = 1 WHERE tenant_id = $2 AND investigation_id = $3`, state, tenant, inv); err != nil {
			t.Fatal(err)
		}
	}
}

// settings writes the DSN directories of the two logins of each tenant and the orchestrator's key directory, and returns the settings that name them.
func (w *wireWorld) settings(t *testing.T) envMap {
	t.Helper()
	recDir, o := t.TempDir(), newOrchDirs(t)
	for tenant, p := range w.rec {
		writeDSN(t, recDir, tenant, p)
	}
	for tenant, p := range w.orch {
		writeDSN(t, o.dsn, tenant, p)
	}
	return envMap{EnvRecordReadDSNDir: recDir, EnvOrchestratorDSNDir: o.dsn, EnvOrchestratorKeyDir: o.keys, EnvOrchestratorKeyMaxActive: "2", EnvReadTimeoutMS: "2000"}
}

func writeDSN(t *testing.T, dir, tenant string, p *pgxpool.Pool) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, tenant), []byte(p.Config().ConnString()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func osEnv(e envMap) Env { return Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir} }

// reader builds the InvestigationReader the way the process does, from the Recorder's stores.
func (w *wireWorld) reader(t *testing.T) (InvestigationReader, func()) {
	t.Helper()
	env := osEnv(w.settings(t))
	records, closeRecords, err := buildRecordReader(t.Context(), env)
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewManual(apiEpoch)
	rec, closeRec, err := buildRecorder(t.Context(), env, clk, ids.New(clk, nil), records)
	if err != nil || rec == nil {
		t.Fatalf("recorder %v, %v", rec, err)
	}
	r, err := buildInvestigationReader(env, rec)
	if err != nil {
		t.Fatal(err)
	}
	return r, func() { closeRec(); closeRecords() }
}

func TestTheReaderServesTheOwnerItsStateInItsOwnTenant(t *testing.T) {
	w := newWireWorld(t)
	r, closer := w.reader(t)
	defer closer()
	for name, c := range map[string]struct {
		tenant, principal, id string
		state                 orchestrator.State
	}{
		"tenant A, the first owner":  {"ten_aa", "prn_1", wireInvA1, "EXECUTING"},
		"tenant A, the second owner": {"ten_aa", "prn_2", wireInvA2, "COMPLETED"},
		"tenant B, its own":          {"ten_bb", "prn_1", wireInvB1, "AWAITING_CONFIRMATION"},
	} {
		state, found, err := r.Read(deadline(t), c.tenant, c.principal, c.id)
		if err != nil || !found || state != c.state {
			t.Errorf("%s: %q, %v, %v; want %q found", name, state, found, err, c.state)
		}
	}
}

// INV-12: another principal's investigation of the same tenant, another tenant's, an absent one, a malformed ID and a row with no principal are all found=false and no error.
func TestTheReaderAnswersNotFoundAndNothingElseForWhatIsNotTheCallers(t *testing.T) {
	w := newWireWorld(t)
	r, closer := w.reader(t)
	defer closer()
	for name, c := range map[string]struct{ tenant, principal, id string }{
		"another principal's, same tenant":   {"ten_aa", "prn_2", wireInvA1},
		"another tenant's":                   {"ten_bb", "prn_1", wireInvA1},
		"another tenant's, its owner's name": {"ten_bb", "prn_2", wireInvA2},
		"absent":                             {"ten_aa", "prn_1", wireInvNo},
		"malformed":                          {"ten_aa", "prn_1", "inv_!!"},
		"a legacy row, for its tenant":       {"ten_aa", "prn_1", wireInvLg},
		"a legacy row, for another":          {"ten_aa", "prn_2", wireInvLg},
		"no principal, an investigation":     {"ten_aa", "", wireInvA1},
		"no principal, a legacy row":         {"ten_aa", "", wireInvLg},
	} {
		state, found, err := r.Read(deadline(t), c.tenant, c.principal, c.id)
		if err != nil || found || state != "" {
			t.Errorf("%s: %q, %v, %v; want not found, no state and no error", name, state, found, err)
		}
	}
}

// A tenant with no login, a tenant that is not a tenant ID and a store that cannot be reached are errors: a failure is never told as "not found", and never falls back to
// another tenant's store (INV-12).
func TestTheReaderFailsWithAnErrorWhenTheStoreCannotAnswer(t *testing.T) {
	w := newWireWorld(t)
	r, closer := w.reader(t)
	for name, tenant := range map[string]string{"a tenant with no login": "ten_zz", "not a tenant ID": "tenant", "empty": ""} {
		state, found, err := r.Read(deadline(t), tenant, "prn_1", wireInvA1)
		if err == nil || found || state != "" {
			t.Errorf("%s: %q, %v, %v; want an error", name, state, found, err)
		}
	}
	closer() // every pool is closed: the database cannot be reached
	state, found, err := r.Read(deadline(t), "ten_aa", "prn_1", wireInvA1)
	if err == nil || found || state != "" {
		t.Fatalf("a closed pool: %q, %v, %v; want an error", state, found, err)
	}
	if msg := err.Error(); strings.Contains(msg, "postgres") || strings.Contains(msg, "orchestrator_rw") || strings.Contains(msg, "127.0.0.1") {
		t.Errorf("the error carries driver text: %s", msg)
	}
}

// The adapter bounds its own read: a request that comes with no deadline is not a read with none.
func TestTheReaderBoundsItsOwnReadWhenTheRequestHasNoDeadline(t *testing.T) {
	w := newWireWorld(t)
	r, closer := w.reader(t)
	defer closer()
	if _, found, err := r.Read(context.Background(), "ten_aa", "prn_1", wireInvA1); err != nil || !found {
		t.Errorf("a context with no deadline: %v, %v; want the investigation", found, err)
	}
}

// bearerAs is a session of one principal.
func bearerAs(iss *edgetest.Issuer, role session.Role, tenant, sub string) string {
	c := edgetest.Good(apiEpoch, role)
	c.Aud, c.Tenant, c.Sub = apiAud, tenant, sub
	return "Bearer " + iss.Token(apiKid, c)
}

// prod builds the components the process builds, from settings, and the protected API on them: what main does, without the listener.
func (w *wireWorld) prod(t *testing.T, iss *edgetest.Issuer) (Components, http.Handler, func()) {
	t.Helper()
	e := w.settings(t)
	for k, v := range sessionEnv(t, iss) {
		e[k] = v
	}
	e[edge.EnvTenants] = "ten_aa,ten_bb,ten_cc"
	clk := clock.NewManual(apiEpoch)
	c, closer, err := buildComponents(t.Context(), osEnv(e), clk, ids.New(clk, nil), discardLog())
	if err != nil {
		t.Fatal(err)
	}
	h, err := buildAPI(c, osEnv(e), clk, ids.New(clk, nil))
	if err != nil || h == nil {
		closer()
		t.Fatalf("api %v, %v", h, err)
	}
	return c, h, closer
}

// The route, end to end through the production components: the verified session, the role gate, the adapter, the orchestrator's login and the database.
func TestTheProductionBuildServesAnInvestigationToItsOwnerAndToNoOneElse(t *testing.T) {
	w := newWireWorld(t)
	iss := edgetest.NewIssuer(t)
	c, h, closer := w.prod(t, iss)
	defer closer()
	if c.Investigations == nil {
		t.Fatal("the investigation reader is not among the components built from the orchestrator's settings")
	}
	own := getInv(h, bearerAs(iss, session.RoleCustomer, "ten_aa", "prn_1"), wireInvA1)
	if own.Code != http.StatusOK || strings.TrimSpace(own.Body.String()) != `{"schema_version":"1.0","state":"working"}` {
		t.Fatalf("the owner: %d %s", own.Code, own.Body.String())
	}
	if rec := getInv(h, bearerAs(iss, session.RoleCustomer, "ten_bb", "prn_1"), wireInvB1); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"needs_confirmation"`) {
		t.Errorf("tenant B's owner: %d %s", rec.Code, rec.Body.String())
	}
	// INV-12: a principal named in the query or a header is not the caller.
	req := httptest.NewRequest(http.MethodGet, "/v1/investigations/"+wireInvA1+"?principal_id=prn_1&principal_ref=prn_1", nil)
	req.Header.Set("Authorization", bearerAs(iss, session.RoleCustomer, "ten_aa", "prn_3"))
	req.Header.Set("X-Principal-Id", "prn_1")
	spoof := httptest.NewRecorder()
	h.ServeHTTP(spoof, req)
	notFound := map[string]*httptest.ResponseRecorder{
		"another principal's":     getInv(h, bearerAs(iss, session.RoleCustomer, "ten_aa", "prn_3"), wireInvA1),
		"another tenant's":        getInv(h, bearerAs(iss, session.RoleCustomer, "ten_bb", "prn_1"), wireInvA1),
		"absent":                  getInv(h, bearerAs(iss, session.RoleCustomer, "ten_aa", "prn_1"), wireInvNo),
		"a row with no principal": getInv(h, bearerAs(iss, session.RoleCustomer, "ten_aa", "prn_1"), wireInvLg),
		"another principal's, but the request names the owner": spoof,
	}
	var want string
	for name, rec := range notFound {
		code, _ := errBody(t, rec)
		if rec.Code != http.StatusNotFound || code != "NOT_FOUND_OR_NOT_AUTHORIZED" {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
		var env map[string]map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		delete(env["error"], "correlation_id")
		got, _ := json.Marshal(env)
		if want == "" {
			want = string(got)
		} else if string(got) != want {
			t.Errorf("%s: a different not-found shape: %s", name, rec.Body.String())
		}
	}
}

// Only a customer reads an investigation (the role gate runs first), and the other roles never reach the database.
func TestTheProductionBuildRefusesOtherRolesAndNoSession(t *testing.T) {
	w := newWireWorld(t)
	iss := edgetest.NewIssuer(t)
	_, h, closer := w.prod(t, iss)
	defer closer()
	for _, role := range []session.Role{session.RoleOps, session.RoleAudit, session.RoleApprover} {
		if rec := getInv(h, bearerAs(iss, role, "ten_aa", "prn_1"), wireInvA1); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", role, rec.Code)
		}
	}
	if rec := getInv(h, "", wireInvA1); rec.Code != http.StatusUnauthorized {
		t.Errorf("no session: %d, want 401", rec.Code)
	}
}

// A store that fails is 503 with the request's correlation ID and no driver text, through the real route.
func TestTheProductionBuildAnswers503WhenTheDatabaseCannotBeReached(t *testing.T) {
	w := newWireWorld(t)
	iss := edgetest.NewIssuer(t)
	_, h, closer := w.prod(t, iss)
	closer() // the pools are closed
	rec := getInv(h, bearerAs(iss, session.RoleCustomer, "ten_aa", "prn_1"), wireInvA1)
	code, corr := errBody(t, rec)
	if rec.Code != http.StatusServiceUnavailable || code != "DEPENDENCY_UNAVAILABLE" || corr == "corr_NONE" || strings.Contains(rec.Body.String(), "postgres") {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
}

// The route table of the production build: which routes exist, for which roles. The reconstruction route is mounted for audit only (A110); the investigation route for
// customers only. POST /v1/conversations is NOT mounted: its Input Gateway has no production constructor in the composition root yet (status/STM-01-wire-investigations.md).
func TestTheProductionRouteTableAndWhoMayCallEachRoute(t *testing.T) {
	w := newWireWorld(t)
	clk := clock.NewManual(apiEpoch)
	c, closer, err := buildComponents(t.Context(), osEnv(w.settings(t)), clk, ids.New(clk, nil), discardLog())
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	roles := map[string][]session.Role{}
	for _, r := range apiRoutes(c, ids.New(clk, nil)) {
		roles[r.Pattern] = r.Roles
	}
	if len(roles) != 2 {
		t.Errorf("routes %v; want exactly the reconstruction and the investigation routes", roles)
	}
	if got := roles["GET /v1/investigations/{id}"]; len(got) != 1 || got[0] != session.RoleCustomer {
		t.Errorf("the investigation route is for %v; want the customer only", got)
	}
	var recon []session.Role
	for p, r := range roles {
		if strings.Contains(p, "reconstructions") {
			recon = r
		}
	}
	if len(recon) != 1 || recon[0] != session.RoleAudit {
		t.Errorf("the reconstruction route is for %v; want audit only", recon)
	}
	if c.Gateway != nil {
		t.Error("a Gateway in the production build: the route of POST /v1/conversations would be mounted by something that is not built")
	}
}

// REQ-AUD-003, A110, A370 as relayed: through the production build, audit reaches the reconstruction handler (it answers as itself: tenant C has no Record reader login, so it
// refuses with its own error and not the role gate's not found), and ops, a customer and an approver never do: each gets the role gate's not-found shape.
func TestTheReconstructionRouteIsReachedByAuditAndNoOneElseInTheProductionBuild(t *testing.T) {
	w := newWireWorld(t)
	iss := edgetest.NewIssuer(t)
	_, h, closer := w.prod(t, iss)
	defer closer()
	const path = "/v1/reconstructions/corr_ABC123"
	rec := call(h, path, bearerAs(iss, session.RoleAudit, "ten_cc", "prn_9"))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "INTERNAL_SAFETY_BLOCK") {
		t.Errorf("audit: %d %s; want the handler's own answer", rec.Code, rec.Body.String())
	}
	for _, role := range []session.Role{session.RoleOps, session.RoleCustomer, session.RoleApprover} {
		rec := call(h, path, bearerAs(iss, role, "ten_cc", "prn_9"))
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "NOT_FOUND_OR_NOT_AUTHORIZED") {
			t.Errorf("%s: %d %s; want the role gate's 404", role, rec.Code, rec.Body.String())
		}
	}
}

// What is built from which settings: with none, no reader; with the Record reader only, the reconstruction route and no investigation reader (it reads over the orchestrator's
// logins); with all, both. A reader is never a typed nil inside the interface (the check of `missing` compares to nil).
func TestTheInvestigationReaderExistsExactlyWhenTheOrchestratorSettingsDo(t *testing.T) {
	w := newWireWorld(t)
	all := w.settings(t)
	clk := clock.NewManual(time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC))
	for name, c := range map[string]struct {
		env  envMap
		want bool
	}{
		"no settings":                    {envMap{}, false},
		"the Record reader only":         {envMap{EnvRecordReadDSNDir: all[EnvRecordReadDSNDir]}, false},
		"the Record reader and the rest": {all, true},
	} {
		got, closer, err := buildComponents(t.Context(), osEnv(c.env), clk, ids.New(clk, nil), discardLog())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if (got.Investigations != nil) != c.want {
			t.Errorf("%s: investigation reader present %v, want %v", name, got.Investigations != nil, c.want)
		}
		closer()
	}
}
