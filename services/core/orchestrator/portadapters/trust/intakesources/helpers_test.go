package intakesources_test

// Shared helpers of the tests. Everything here is SYNTHETIC (AGENT-25). The database is a real PostgreSQL with every migration, one trust_rw login for each of two tenants
// (internal/testpg), so the database, not the reader's code, binds each read to the tenant (migration 0026: row-level security through the login's own mapping). The intents
// are written by the migrating login with an explicit admission time, so that no test depends on the server's clock.

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/intakesources"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/trust/policystore"
)

const (
	tenantA, tenantB = "ten_aa", "ten_bb"
	node             = "fin.payments.exception.investigate"
	readTimeout      = 20 * time.Second
)

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return c
}

// resolvedSlots is the slots of an intent whose subject is resolved, with a slot of another type beside it.
const resolvedSlots = `{"subject_resource":{"type":"Resource","status":"RESOLVED","resolved_ref":"fin.transaction:TX88213"},"question_type":{"type":"Reference","value":"STATUS"}}`

type world struct {
	admin *pgxpool.Pool
	trust map[string]*pgxpool.Pool
	calls int // how many times a login was asked for
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	w := &world{admin: admin, trust: map[string]*pgxpool.Pool{}}
	for _, id := range []string{tenantA, tenantB} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Synthetic')`, id); err != nil {
			t.Fatal(err)
		}
		w.trust[id] = testpg.TenantLogin(t, admin, dsn, "trust_rw", id)
	}
	return w
}

// For is the tenant's own Trust login, and no other: an unknown tenant has none.
func (w *world) For(tenantID string) (intakesources.Querier, error) {
	w.calls++
	p, ok := w.trust[tenantID]
	if !ok {
		return nil, errors.New("no login for this tenant")
	}
	return p, nil
}

// publications is the same logins as the policy publication store reads them.
type publications struct{ w *world }

func (p publications) For(tenantID string) (policystore.Querier, error) {
	l, ok := p.w.trust[tenantID]
	if !ok {
		return nil, errors.New("no login for this tenant")
	}
	return l, nil
}

// intent stores one version of an intent as the migrating login, admitted at the time given. The chain of one intent shares a correlation; slots is the document's slots.
func (w *world) intent(t *testing.T, tenant, id string, version int, nodeID string, at time.Time, slots string) {
	t.Helper()
	corr := "corr_" + id[len("int_"):]
	var sup any
	supDoc := ""
	if version > 1 {
		sup = fmt.Sprintf("%s@%d", id, version-1)
		supDoc = fmt.Sprintf(`,"supersedes":%q`, sup)
	}
	doc := fmt.Sprintf(`{"intent_id":%q,"tenant_id":%q,"correlation_id":%q,"version":%d,"status":"VALIDATED","intent_node":%q%s,"slots":%s}`, id, tenant, corr, version, nodeID, supDoc, slots)
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO admitted_intent (tenant_id, intent_id, version, supersedes, intent_node, intent_hash, correlation_id, json, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9)`, tenant, id, version, sup, nodeID, fmt.Sprintf("sha256:%064d", version), corr, doc, at); err != nil {
		t.Fatalf("intent: %v", err)
	}
}

// failingRow and failingLogins give a Querier whose read fails with an error that names a server, to prove that no driver text reaches an error.
type failingRow struct{ err error }

func (r failingRow) Scan(...any) error { return r.err }

type failingLogin struct{ err error }

func (f failingLogin) QueryRow(context.Context, string, ...any) pgx.Row { return failingRow{f.err} }

type loginFunc func(tenantID string) (intakesources.Querier, error)

func (f loginFunc) For(tenantID string) (intakesources.Querier, error) { return f(tenantID) }

// blockingLogin answers a read only when its context is over, as a database that hangs does.
type blockingRow struct{ ctx context.Context }

func (r blockingRow) Scan(...any) error { <-r.ctx.Done(); return r.ctx.Err() }

type blockingLogin struct{}

func (blockingLogin) QueryRow(c context.Context, _ string, _ ...any) pgx.Row { return blockingRow{c} }
