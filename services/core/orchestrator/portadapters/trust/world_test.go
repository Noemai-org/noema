package trustport_test

// The test world of the store adapters (STM-01 follow-up; REQ-TRU-003, REQ-SEC-004; INV-11, INV-12): a real PostgreSQL with every migration, two tenants, and one login per tenant
// and power. The intents are made by the real gate and saved by the real store, so the stored documents are the ones Trust reads in production. All data is SYNTHETIC (AGENT-25).

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/recordsig"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/understanding"
	"github.com/Noemai-org/noema/services/core/understanding/intentstore"
)

func TestMain(m *testing.M) { testpg.Main(m) }

const (
	tenantA = "ten_aa"
	tenantB = "ten_bb"
	user    = "usr_88213"
)

// corrOf is the correlation an intent is admitted under: the store keeps one intent chain per investigation, so each fixture intent has its own.
func corrOf(intentID string) string { return "corr_" + intentID[len("int_"):] }

func tctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return c
}

func fintechPack(t testing.TB) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../../../../packs/fintech_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type world struct {
	t     *testing.T
	admin *pgxpool.Pool
	dsn   string
	clk   *clock.Manual
	sign  map[string]chain.Signer
	pools map[string]*pgxpool.Pool
	seq   int // the next number of a synthetic row a test inserts (rates_test.go)
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(tctx(t), admin, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	w := &world{t: t, admin: admin, dsn: dsn, clk: clock.NewManual(time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)), pools: map[string]*pgxpool.Pool{}}
	w.sign = recordsig.All(t, admin, "trustport", nil)
	for _, id := range []string{tenantA, tenantB} {
		if _, err := admin.Exec(tctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Synthetic')`, id); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

// as is the tenant's own login of one power (a group role), made on first use.
func (w *world) as(group, tenant string) *pgxpool.Pool {
	w.t.Helper()
	k := group + "/" + tenant
	if w.pools[k] == nil {
		w.pools[k] = testpg.TenantLogin(w.t, w.admin, w.dsn, group, tenant)
	}
	return w.pools[k]
}

// trustPools is what the composition has: one trust_rw login per tenant.
func (w *world) trustPools() trustport.TenantPools {
	return trustport.PoolsOf(map[string]*pgxpool.Pool{tenantA: w.as("trust_rw", tenantA), tenantB: w.as("trust_rw", tenantB)})
}

func (w *world) intents(pools trustport.TenantPools, pack []byte) *trustport.StoredIntents {
	w.t.Helper()
	s, err := trustport.NewStoredIntents(pools, pack, 5*time.Second)
	if err != nil {
		w.t.Fatal(err)
	}
	return s
}

func (w *world) killSwitch(pools trustport.TenantPools) *trustport.KillSwitch {
	w.t.Helper()
	k, err := trustport.NewKillSwitch(pools, "fin@1.0.0", 5*time.Second)
	if err != nil {
		w.t.Fatal(err)
	}
	return k
}

func proposal(tenant, intentID string, version int, mutate func(m map[string]any)) []byte {
	m := map[string]any{
		"schema_version": "3.0", "intent_id": intentID, "version": version, "supersedes": nil, "tenant_id": tenant, "correlation_id": corrOf(intentID), "created_at": "2026-10-01T09:47:03Z",
		"principal":   map[string]any{"principal_id": user, "principal_type": "unset", "authn_ref": "ses_7a1b", "asserted_by": "unset"},
		"source":      map[string]any{"mode": "MODE_1", "channel": "text", "locale_detected": []any{"en-NG"}, "asr_confidence": nil, "raw_ref": "raw_01J9X3"},
		"pack":        map[string]any{"pack_id": "fin", "pack_version": "1.0.0"},
		"intent_node": "fin.payments.exception.investigate",
		"slots": map[string]any{
			"subject_resource": map[string]any{"type": "Resource", "status": "RESOLVED", "resolved_ref": "fin.transaction:TX88213"},
			"question_type":    map[string]any{"type": "Reference", "value": "STATUS"},
		},
		"ambiguity_flags": []any{}, "desired_outcome_refs": []any{"fin.outcome.exception.status_explained"}, "status": "VALIDATED", "sig": map[string]any{"key_id": "unsigned", "value": "unsigned"},
	}
	if version > 1 {
		m["supersedes"] = intentID + "@" + string(rune('0'+version-1))
	}
	if mutate != nil {
		mutate(m)
	}
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return b
}

// save admits a proposal with the real gate (over the pack) and stores it with the real store, as the tenant's orchestrator login. It returns what the gate computed.
func (w *world) save(pack []byte, tenant string, raw []byte) understanding.AdmittedIntent {
	w.t.Helper()
	view, err := understanding.NewPackView(pack)
	if err != nil {
		w.t.Fatal(err)
	}
	gate, err := understanding.NewGate(64<<10, view)
	if err != nil {
		w.t.Fatal(err)
	}
	adm, err := gate.Admit(raw, understanding.Session{PrincipalID: user, TenantID: tenant, AuthnRef: "ses_7a1b", PrincipalType: "end_user", AssertedBy: "enterprise_idp"}, corrOfRaw(w.t, raw))
	if err != nil {
		w.t.Fatalf("the gate refused the fixture: %v", err)
	}
	o := adm.Intent.Object()
	sup := ""
	if o.Supersedes != nil {
		sup = string(*o.Supersedes)
	}
	st := &intentstore.Store{Pool: w.as("orchestrator_rw", tenant), Appender: &chain.Appender{Clock: w.clk, IDs: ids.New(w.clk, nil), Signers: w.sign}, Timeout: 5 * time.Second}
	if _, err := st.Save(tctx(w.t), "customer:"+user, intentstore.Admitted{TenantID: tenant, IntentID: string(o.IntentId), Version: int(o.Version), Supersedes: sup,
		IntentNode: string(o.IntentNode), IntentHash: adm.Intent.Hash(), CorrelationID: string(o.CorrelationId), JSON: adm.Intent.JSON()}); err != nil {
		w.t.Fatalf("save: %v", err)
	}
	return adm.Intent
}

func corrOfRaw(t testing.TB, raw []byte) string {
	t.Helper()
	var head struct {
		IntentID string `json:"intent_id"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		t.Fatal(err)
	}
	return corrOf(head.IntentID)
}
