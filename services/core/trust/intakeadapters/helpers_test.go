package intakeadapters

// Shared helpers of the adapter tests. Everything here is SYNTHETIC. The pack is the shipped fintech pack; the engine is the real one; the publication store is a real PostgreSQL
// read as each tenant's own Trust login (internal/testpg).

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/policystore"
)

func TestMain(m *testing.M) { testpg.Main(m) }

const (
	tenantA, tenantB = "ten_aa", "ten_bb"
	node             = "fin.payments.exception.investigate"
	policyID         = "fin.pol"
)

// the times of the tests, in Unix seconds: three publications and a reading after them
var (
	t1   = time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC).Unix()
	t2   = time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC).Unix()
	t3   = time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC).Unix()
	late = time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC).Unix()
)

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return c
}

func realPack(t testing.TB) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../../../packs/fintech_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// variantPack is the shipped pack with its policy at another version and its rules edited. edit may be nil.
func variantPack(t testing.TB, version string, edit func(rules []any) []any) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(realPack(t), &m); err != nil {
		t.Fatal(err)
	}
	p := m["policy"].(map[string]any)
	p["version"] = version
	if edit != nil {
		p["rules"] = edit(p["rules"].([]any))
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func rule(id, class, evalTime, target, cond, effect, reason string, params map[string]any) map[string]any {
	r := map[string]any{"rule_id": id, "class": class, "evaluation_time": evalTime, "applies_to": []any{target}, "condition": cond, "effect": effect, "reason_code": reason}
	if params != nil {
		r["params"] = params
	}
	return r
}

func withRules(extra ...map[string]any) func([]any) []any {
	return func(rules []any) []any {
		for _, r := range extra {
			rules = append(rules, r)
		}
		return rules
	}
}

func book(t testing.TB, packs ...[]byte) *PolicyBook {
	t.Helper()
	b, err := LoadPolicyBook(packs...)
	if err != nil {
		t.Fatalf("LoadPolicyBook: %v", err)
	}
	return b
}

type world struct {
	admin  *pgxpool.Pool
	logins map[string]*pgxpool.Pool
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	w := &world{admin: admin, logins: map[string]*pgxpool.Pool{}}
	for _, id := range []string{tenantA, tenantB} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'T')`, id); err != nil {
			t.Fatal(err)
		}
		w.logins[id] = testpg.TenantLogin(t, admin, dsn, "trust_rw", id)
	}
	return w
}

// For is the tenant's own Trust login.
func (w *world) For(tenantID string) (policystore.Querier, error) {
	p, ok := w.logins[tenantID]
	if !ok {
		return nil, errors.New("no login for this tenant")
	}
	return p, nil
}

// publish writes a publication with a time of its own as the tenant's Trust login; the guard still decides who may write and which version is higher.
func (w *world) publish(t *testing.T, tenant, version string, at int64) {
	t.Helper()
	if err := w.tryPublish(tenant, version, at); err != nil {
		t.Fatalf("publish %s@%s: %v", policyID, version, err)
	}
}

func (w *world) tryPublish(tenant, version string, at int64) error {
	_, err := w.logins[tenant].Exec(context.Background(), `INSERT INTO policy_publication (tenant_id, policy_id, version, version_key, published_by, published_at, document_hash)
		VALUES ($1, $2, $3, policy_version_key($3), 'adm_synth01', to_timestamp($4), 'sha256:00000000000000000000000000000000000000000000000000000000000000aa')`, tenant, policyID, version, at)
	return err
}

// adm answers one admission time for every intent, or an error.
type adm struct {
	at    int64
	err   error
	calls int
}

func (a *adm) AdmittedAt(_ context.Context, _, _ string, _ int) (int64, error) {
	a.calls++
	return a.at, a.err
}

// factCall is one call a FactSource received.
type factCall struct {
	q    intake.PolicyQuery
	need FactNeed
}

// facts is a FactSource over a master set: it returns what the adapter asked for and nothing else, so a need the adapter did not ask for is an absence.
type facts struct {
	master FactSet
	err    error
	extra  func(FactSet) FactSet // changes the answer after it is filtered, to give the adapter something it must refuse
	calls  []factCall
}

func resolvedMaster() FactSet {
	s := FactSet{Variables: map[string]predicate.Value{
		"subject_resource": predicate.Ref("txn_1"), "subject_resource.status": predicate.Enum("RESOLVED"),
		"logical_resolution_epoch": predicate.Int(1), "case_id": predicate.Ref("case_1")},
		Entities: map[string][]predicate.Row{}}
	for _, e := range []string{"transaction", "beneficiary", "account", "switch_event", "settlement_record", "case", "attachment", "complaint_ref"} {
		s.Entities[e] = []predicate.Row{}
	}
	return s
}

func (f *facts) PolicyFacts(_ context.Context, q intake.PolicyQuery, need FactNeed) (FactSet, error) {
	f.calls = append(f.calls, factCall{q, need})
	if f.err != nil {
		return FactSet{}, f.err
	}
	out := FactSet{Variables: map[string]predicate.Value{}, Entities: map[string][]predicate.Row{}}
	for _, v := range need.Variables {
		if val, ok := f.master.Variables[v]; ok {
			out.Variables[v] = val
		}
	}
	for _, e := range need.Entities {
		if rows, ok := f.master.Entities[e]; ok {
			out.Entities[e] = rows
		}
	}
	if f.extra != nil {
		out = f.extra(out)
	}
	return out, nil
}

func (w *world) policies(t *testing.T, b *PolicyBook, a Admissions, f FactSource) *Policies {
	t.Helper()
	p, err := NewPolicies(PoliciesConfig{Book: b, Publications: StorePublications{DBs: w}, Admissions: a, Facts: f})
	if err != nil {
		t.Fatalf("NewPolicies: %v", err)
	}
	return p
}

func query(capability string, now int64) intake.PolicyQuery {
	return intake.PolicyQuery{TenantID: tenantA, IntentID: "int_aa1", IntentVersion: 1, IntentNode: node, CapabilityID: capability, SnapshotID: "snap_1", Now: now}
}
