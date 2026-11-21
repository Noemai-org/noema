package intakeadapters

// Shared helpers of the adapter tests. Everything here is SYNTHETIC. The pack is the shipped fintech pack; the engine is the real one; the publication store is a real PostgreSQL
// read as each tenant's own Trust login (internal/testpg).

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/policy"
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
	for _, raw := range packs {
		if version, hash, ok := tryPackPolicyHash(raw); ok {
			packHashes.Store(version, hash)
		}
	}
	return b
}

// packHashes is the document hash of each policy version of the packs a test loaded, worked out here from the pack's own bytes, so that a publication made by the helpers below
// rests on the document the pack carries (A413). It is a test fixture and never read by the code under test.
var packHashes sync.Map

// packPolicyHash is the version of the pack's policy and its document hash, computed independently of the loader.
func packPolicyHash(t testing.TB, raw []byte) (version, hash string) {
	t.Helper()
	version, hash, ok := tryPackPolicyHash(raw)
	if !ok {
		t.Fatal("the pack's policy has no document hash")
	}
	return version, hash
}

// tryPackPolicyHash is false for a policy that has no single reading or is not made of integers (a document policypublish refuses and no row can rest on).
func tryPackPolicyHash(raw []byte) (version, hash string, ok bool) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return "", "", false
	}
	var head struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(top["policy"], &head); err != nil {
		return "", "", false
	}
	h, err := policy.DocumentHash(top["policy"])
	if err != nil {
		return "", "", false
	}
	return head.Version, h, true
}

// documentHashOf is the hash the helpers publish a version with: the hash of the document of that version in the packs loaded so far, or a hash that matches nothing.
func documentHashOf(version string) string {
	if h, ok := packHashes.Load(version); ok {
		return h.(string)
	}
	return "sha256:00000000000000000000000000000000000000000000000000000000000000aa"
}

type world struct {
	admin   *pgxpool.Pool
	logins  map[string]*pgxpool.Pool
	pending []pendingPublication // publications a test asked for, written when the first read needs them (see flush)
	t       *testing.T
	named   map[string]bool // tenant@version of a publication whose hash a test named (publishHashed): align leaves it alone
}

type pendingPublication struct {
	tenant, version string
	at              int64
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
	w.flush()
	p, ok := w.logins[tenantID]
	if !ok {
		return nil, errors.New("no login for this tenant")
	}
	return p, nil
}

// publish writes a publication with a time of its own as the tenant's Trust login; the guard still decides who may write and which version is higher.
func (w *world) publish(t *testing.T, tenant, version string, at int64) {
	t.Helper()
	w.t = t
	w.pending = append(w.pending, pendingPublication{tenant, version, at})
}

// flush writes the pending publications in the order they were asked for, each with the document hash of its version in the packs the test has loaded by now (a test publishes
// first and loads its packs after, so the hash is read when the first read needs the rows, not when the test asks for them).
func (w *world) flush() {
	pending := w.pending
	w.pending = nil
	for _, p := range pending {
		if err := w.tryPublishHashed(p.tenant, p.version, p.at, documentHashOf(p.version)); err != nil {
			w.t.Fatalf("publish %s@%s: %v", policyID, p.version, err)
		}
	}
}

// publishHashed writes a publication with the hash a test names (nil is a NULL hash: the guard is switched off for that one insert, as the administrator, the way a row from
// before migration 0035 exists).
func (w *world) publishHashed(t *testing.T, tenant, version string, at int64, hash *string) {
	t.Helper()
	w.t = t
	w.flush()
	if w.named == nil {
		w.named = map[string]bool{}
	}
	w.named[tenant+"@"+version] = true
	if hash == nil {
		if _, err := w.admin.Exec(context.Background(), `ALTER TABLE policy_publication DISABLE TRIGGER policy_publication_guard_document_hash`); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := w.admin.Exec(context.Background(), `ALTER TABLE policy_publication ENABLE TRIGGER policy_publication_guard_document_hash`); err != nil {
				t.Fatal(err)
			}
		}()
		if _, err := w.logins[tenant].Exec(context.Background(), `INSERT INTO policy_publication (tenant_id, policy_id, version, version_key, published_by, published_at)
			VALUES ($1, $2, $3, policy_version_key($3), 'adm_synth01', to_timestamp($4))`, tenant, policyID, version, at); err != nil {
			t.Fatalf("publish %s@%s with no hash: %v", policyID, version, err)
		}
		return
	}
	if err := w.tryPublishHashed(tenant, version, at, *hash); err != nil {
		t.Fatalf("publish %s@%s: %v", policyID, version, err)
	}
}

func (w *world) tryPublishHashed(tenant, version string, at int64, hash string) error {
	_, err := w.logins[tenant].Exec(context.Background(), `INSERT INTO policy_publication (tenant_id, policy_id, version, version_key, published_by, published_at, document_hash)
		VALUES ($1, $2, $3, policy_version_key($3), 'adm_synth01', to_timestamp($4), $5)`, tenant, policyID, version, at, hash)
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
	w.t = t
	w.flush()
	w.align()
	p, err := NewPolicies(PoliciesConfig{Book: b, Publications: StorePublications{DBs: w}, Admissions: a, Facts: f})
	if err != nil {
		t.Fatalf("NewPolicies: %v", err)
	}
	return p
}

func query(capability string, now int64) intake.PolicyQuery {
	return intake.PolicyQuery{TenantID: tenantA, IntentID: "int_aa1", IntentVersion: 1, IntentNode: node, CapabilityID: capability, SnapshotID: "snap_1", Now: now}
}

// align gives each publication whose hash no test named the hash of the document its version has in the packs loaded last. Many tests publish one row and then try several packs of
// that version (a rule edited, a wait changed): the row stands for "published with the document under test". Rows are insert-only, so the fixture rewrites the hash as the
// administrator with the table's triggers off, and puts them back on. A test that wants a row with another hash, or none, names it with publishHashed and align leaves it.
func (w *world) align() {
	rows, err := w.admin.Query(context.Background(), `SELECT tenant_id, version FROM policy_publication`)
	if err != nil {
		w.t.Fatal(err)
	}
	type key struct{ tenant, version string }
	var keys []key
	for rows.Next() {
		var k key
		if err := rows.Scan(&k.tenant, &k.version); err != nil {
			w.t.Fatal(err)
		}
		keys = append(keys, k)
	}
	rows.Close()
	if _, err := w.admin.Exec(context.Background(), `ALTER TABLE policy_publication DISABLE TRIGGER USER`); err != nil {
		w.t.Fatal(err)
	}
	defer func() {
		if _, err := w.admin.Exec(context.Background(), `ALTER TABLE policy_publication ENABLE TRIGGER USER`); err != nil {
			w.t.Fatal(err)
		}
	}()
	for _, k := range keys {
		if w.named[k.tenant+"@"+k.version] {
			continue
		}
		if h, ok := packHashes.Load(k.version); ok {
			if _, err := w.admin.Exec(context.Background(), `UPDATE policy_publication SET document_hash = $1 WHERE tenant_id = $2 AND version = $3`, h.(string), k.tenant, k.version); err != nil {
				w.t.Fatal(err)
			}
		}
	}
}
