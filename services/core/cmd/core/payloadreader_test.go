package main

// STM-01 / EXP-01 / EVD: the PayloadReader over the Trust login (ruling A228, relayed, Founder-approved interim, not yet committed; noema#596: C2's Reconciler reads the recorded
// payload of an evidence item by (tenant_id, payload_ref)). Pools come from a directory of file-mounted DSNs of each tenant's trust_rw login, as the Record reader's and the
// Recorder's do (A170, A202). The database, not this code, decides which tenant's rows a connection can read (migration 0008, DEBT-04); this code refuses to hand out any pool
// but the tenant's own, and a payload another tenant's login cannot see reads as absent. REQ-CTX-001, REQ-SEC-004, INV-12, AGENT-13, AGENT-16. All data is SYNTHETIC.

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/db"
)

func payloadEnv(dir string) Env {
	e := envMap{EnvTrustDSNDir: dir}
	return Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}
}

// With the setting absent there is no payload reader and no error: the Reconciler is then not configured and /readyz says so.
func TestWithNoTrustDSNDirectoryThereIsNoPayloadReader(t *testing.T) {
	r, closer, err := buildPayloadReader(t.Context(), Env{Getenv: envMap{}.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir})
	if err != nil || r != nil || closer == nil {
		t.Fatalf("got %v, %v, closer nil %v; want no reader, no error and a closer that does nothing", r, err, closer == nil)
	}
	closer()
}

// Each file is one tenant's pool; a tenant with no file has none and never falls back to another's; no error carries a DSN.
func TestEachTrustFileIsOneTenantsPoolAndAMissingTenantHasNone(t *testing.T) {
	dir, _ := dsnDir(t, map[string]string{"ten_aa": synthDSN("trust_rw_aa") + "\n", "ten_bb": synthDSN("trust_rw_bb")})
	r, closer, err := buildPayloadReader(t.Context(), payloadEnv(dir))
	if err != nil || r == nil {
		t.Fatalf("got %v, %v", r, err)
	}
	defer closer()
	if _, err := r.Read(deadline(t), "ten_cc", "pay_AA11"); !errors.Is(err, ErrNoPayloadPool) {
		t.Errorf("a tenant with no file: err %v, want ErrNoPayloadPool", err)
	}
}

func TestABadTrustDSNDirectoryRefusesToStartAndNeverEchoesAnyDSN(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"an empty directory":        {},
		"a file not named a tenant": {"trust": synthDSN("t")},
		"an empty file":             {"ten_aa": "   \n"},
		"a DSN that does not parse": {"ten_aa": "postgres://u:" + synthPassword + "@[::1/db"},
		"one login in two files":    {"ten_aa": synthDSN("trust_rw"), "ten_bb": synthDSN("trust_rw")},
	} {
		t.Run(name, func(t *testing.T) {
			dir, _ := dsnDir(t, files)
			r, closer, err := buildPayloadReader(t.Context(), payloadEnv(dir))
			if err == nil || r != nil || closer != nil {
				t.Fatalf("got %v, %v, closer nil %v; want an error and nothing built", r, err, closer == nil)
			}
			if strings.Contains(err.Error(), synthPassword) || strings.Contains(err.Error(), "postgres://") {
				t.Errorf("the error carries a DSN: %v", err)
			}
		})
	}
}

func deadline(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

type payloadWorld struct {
	r *trustPayloads
	// evidence logins write what the reader reads, by tenant
	ev map[string]*pgxpool.Pool
}

func newPayloadWorld(t *testing.T) *payloadWorld {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(deadline(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	w := &payloadWorld{r: &trustPayloads{pools: map[string]*pgxpool.Pool{}}, ev: map[string]*pgxpool.Pool{}}
	for _, tenant := range []string{"ten_aa", "ten_bb"} {
		if _, err := admin.Exec(deadline(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Synthetic bank')`, tenant); err != nil {
			t.Fatal(err)
		}
		w.ev[tenant] = testpg.TenantLogin(t, admin, dsn, "evidence_rw", tenant)
		w.r.pools[tenant] = testpg.TenantLogin(t, admin, dsn, "trust_rw", tenant)
	}
	return w
}

func (w *payloadWorld) put(t *testing.T, tenant, ref, doc string) {
	t.Helper()
	_, err := w.ev[tenant].Exec(deadline(t), `INSERT INTO evidence_payload (payload_ref, tenant_id, json, raw_hash, sensitivity, created_at)
		VALUES ($1, $2, $3::jsonb, 'sha256:`+strings.Repeat("a", 64)+`', 'S2_internal', '2026-11-15T09:00:00Z')`, ref, tenant, doc)
	if err != nil {
		t.Fatal(err)
	}
}

// The reader returns the tenant's own recorded payload by reference, as the JSON that was stored.
func TestThePayloadOfATenantIsReadByItsReference(t *testing.T) {
	w := newPayloadWorld(t)
	w.put(t, "ten_aa", "pay_AA11", `{"semantic_status": "SUCCESS", "entries": [{"leg": "debit"}]}`)
	got, err := w.r.Read(deadline(t), "ten_aa", "pay_AA11")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"semantic_status"`) || !strings.Contains(string(got), `"SUCCESS"`) {
		t.Fatalf("got %s", got)
	}
}

// INV-12, REQ-SEC-004: a reference of another tenant reads as absent through this tenant's login (row-level security), and asking tenant B's name through tenant A's pool
// finds nothing either: the tenant argument is never the thing that isolates.
func TestAnotherTenantsPayloadIsAbsentNeverReadable(t *testing.T) {
	w := newPayloadWorld(t)
	w.put(t, "ten_bb", "pay_BB22", `{"secret": "other tenant"}`)
	if _, err := w.r.Read(deadline(t), "ten_aa", "pay_BB22"); !errors.Is(err, ErrPayloadNotFound) {
		t.Errorf("tenant A reading tenant B's reference: err %v, want ErrPayloadNotFound", err)
	}
	// A pool of tenant A used for tenant B's name: the world must never do this, and if it does the database still shows nothing.
	crossed := &trustPayloads{pools: map[string]*pgxpool.Pool{"ten_bb": w.r.pools["ten_aa"]}}
	if b, err := crossed.Read(deadline(t), "ten_bb", "pay_BB22"); !errors.Is(err, ErrPayloadNotFound) {
		t.Errorf("tenant A's login asked for tenant B's payload: %s, %v; want ErrPayloadNotFound", b, err)
	}
	if _, err := w.r.Read(deadline(t), "ten_aa", "pay_ZZ99"); !errors.Is(err, ErrPayloadNotFound) {
		t.Errorf("an unknown reference: err %v, want ErrPayloadNotFound", err)
	}
}

// A call that cannot be answered safely is refused before the database is asked: no deadline, no tenant, a reference that is not a plain reference.
func TestAnUnusableCallIsRefusedBeforeTheDatabaseIsAsked(t *testing.T) {
	w := newPayloadWorld(t)
	w.put(t, "ten_aa", "pay_AA11", `{"ok": true}`)
	for name, call := range map[string]func() error{
		"no deadline":            func() error { _, e := w.r.Read(context.Background(), "ten_aa", "pay_AA11"); return e },
		"no tenant":              func() error { _, e := w.r.Read(deadline(t), "", "pay_AA11"); return e },
		"a tenant not a tenant":  func() error { _, e := w.r.Read(deadline(t), "ten_aa' OR '1'='1", "pay_AA11"); return e },
		"no reference":           func() error { _, e := w.r.Read(deadline(t), "ten_aa", ""); return e },
		"a reference with space": func() error { _, e := w.r.Read(deadline(t), "ten_aa", "pay AA11"); return e },
		"a reference with a control character": func() error {
			_, e := w.r.Read(deadline(t), "ten_aa", "pay_AA11\x00")
			return e
		},
		"an over-long reference": func() error { _, e := w.r.Read(deadline(t), "ten_aa", "pay_"+strings.Repeat("A", 200)); return e },
	} {
		if err := call(); !errors.Is(err, ErrPayloadUnusable) {
			t.Errorf("%s: err %v, want ErrPayloadUnusable", name, err)
		}
	}
	var nilReader *trustPayloads
	if _, err := nilReader.Read(deadline(t), "ten_aa", "pay_AA11"); !errors.Is(err, ErrPayloadUnusable) {
		t.Errorf("a nil reader: err %v, want ErrPayloadUnusable", err)
	}
}
