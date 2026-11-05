package layer_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/db"
)

// Tests for migration 0008 (EVD-01 slice 2a): evidence_item and evidence_payload, PERD 38.2. REQ-CTX-001, INV-10, INV-19, INV-11.
// All data is SYNTHETIC.

const (
	tenantA   = "ten_aa"
	denied    = "42501"
	violation = "23" // an integrity constraint or an immutability trigger (class 23)
	hashA     = "sha256:aaaa111122223333aaaa111122223333aaaa111122223333aaaa111122223333"
)

func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return c
}

func code(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

type world struct {
	admin *pgxpool.Pool
	dsn   string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, id := range []string{tenantA, "ten_bb"} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Synthetic')`, id); err != nil {
			t.Fatal(err)
		}
	}
	return &world{admin: admin, dsn: dsn}
}

func (w *world) as(t *testing.T, role string) *pgxpool.Pool {
	t.Helper()
	p, err := db.OpenAsRole(ctx(t), w.dsn, role)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

func insertPayload(p *pgxpool.Pool, c context.Context, tenant, ref string) error {
	_, err := p.Exec(c, `INSERT INTO evidence_payload (payload_ref, tenant_id, json, raw_hash, sensitivity, created_at)
		VALUES ($1, $2, '{"synthetic": true}', $3, 'S2_internal', '2026-11-05T09:00:00Z')`, ref, tenant, hashA)
	return err
}

func insertItem(p *pgxpool.Pool, c context.Context, tenant, id, ref, status, hash string, freshness int) error {
	_, err := p.Exec(c, `INSERT INTO evidence_item (evidence_id, tenant_id, correlation_id, source, source_record_id, read_by, read_at, effective_at,
		freshness_sla_seconds, authority_class, resource_scope, principal_scope, sensitivity, fact_status, payload_hash, payload_ref, created_at)
		VALUES ($1, $2, 'cor_synthetic', 'fin.src.core_ledger', 'rec-1', 'TRUST', '2026-11-05T09:00:00Z', '2026-11-05T08:59:00Z',
		$6, 'AUTHORITATIVE_FOR:debit_posted_or_reversed', 'res_synthetic', 'prn_synthetic', 'S2_internal', $4, $5, $3, '2026-11-05T09:00:00Z')`,
		id, tenant, ref, status, hash, freshness)
	return err
}

// REQ-CTX-001: the Evidence Layer persists a payload and then the item that points at it.
func TestEvidenceRwStoresAPayloadThenItsItem(t *testing.T) {
	w := newWorld(t)
	ev := w.as(t, "evidence_rw")
	if err := insertPayload(ev, ctx(t), tenantA, "pay_1"); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if err := insertItem(ev, ctx(t), tenantA, "evd_1", "pay_1", "KNOWN", hashA, 300); err != nil {
		t.Fatalf("item: %v", err)
	}
}

// REQ-CTX-001: no payload and no item is mutable after insert (a database trigger, not a convention).
func TestEvidenceRowsAreImmutable(t *testing.T) {
	w := newWorld(t)
	ev := w.as(t, "evidence_rw")
	if err := insertPayload(ev, ctx(t), tenantA, "pay_1"); err != nil {
		t.Fatal(err)
	}
	if err := insertItem(ev, ctx(t), tenantA, "evd_1", "pay_1", "KNOWN", hashA, 300); err != nil {
		t.Fatal(err)
	}
	for name, q := range map[string]string{
		"update an item":    `UPDATE evidence_item SET fact_status = 'UNKNOWN' WHERE evidence_id = 'evd_1'`,
		"delete an item":    `DELETE FROM evidence_item WHERE evidence_id = 'evd_1'`,
		"truncate items":    `TRUNCATE evidence_item CASCADE`,
		"update a payload":  `UPDATE evidence_payload SET json = '{}' WHERE payload_ref = 'pay_1'`,
		"delete a payload":  `DELETE FROM evidence_payload WHERE payload_ref = 'pay_1'`,
		"truncate payloads": `TRUNCATE evidence_payload CASCADE`,
	} {
		// The administrator is the strongest caller there is: only the trigger can stop it.
		if _, err := w.admin.Exec(ctx(t), q); err == nil || !strings.HasPrefix(code(err), violation) {
			t.Errorf("%s by the administrator: err = %v (code %q), want an integrity error", name, err, code(err))
		}
	}
}

// INV-19, REQ-CTX-002: a status outside the eight, a "not credited" status and a hash that is not sha256 are refused by the table.
func TestTheTableRefusesAnInvalidItem(t *testing.T) {
	w := newWorld(t)
	ev := w.as(t, "evidence_rw")
	if err := insertPayload(ev, ctx(t), tenantA, "pay_1"); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		status, hash string
		freshness    int
	}{
		"a negative fact status":  {"NOT_CREDITED", hashA, 300},
		"a lower-case status":     {"known", hashA, 300},
		"a hash in upper case":    {"KNOWN", strings.ToUpper(hashA), 300},
		"a hash without the tag":  {"KNOWN", strings.TrimPrefix(hashA, "sha256:"), 300},
		"a zero freshness window": {"KNOWN", hashA, 0},
	}
	i := 0
	for name, c := range cases {
		i++
		err := insertItem(ev, ctx(t), tenantA, "evd_bad_"+string(rune('a'+i)), "pay_1", c.status, c.hash, c.freshness)
		if err == nil || !strings.HasPrefix(code(err), violation) {
			t.Errorf("%s: err = %v (code %q), want a constraint violation", name, err, code(err))
		}
	}
}

// INV-11, REQ-SEC-004: an item points only at a payload of its own tenant, and only at one that exists.
func TestAnItemPointsOnlyAtItsOwnTenantsPayload(t *testing.T) {
	w := newWorld(t)
	ev := w.as(t, "evidence_rw")
	if err := insertPayload(ev, ctx(t), "ten_bb", "pay_other"); err != nil {
		t.Fatal(err)
	}
	if err := insertItem(ev, ctx(t), tenantA, "evd_x", "pay_other", "KNOWN", hashA, 300); err == nil || !strings.HasPrefix(code(err), violation) {
		t.Errorf("an item of tenant A pointing at tenant B's payload: err = %v", err)
	}
	if err := insertItem(ev, ctx(t), tenantA, "evd_y", "pay_missing", "KNOWN", hashA, 300); err == nil || !strings.HasPrefix(code(err), violation) {
		t.Errorf("an item without a payload: err = %v", err)
	}
}

// BD-05, REQ-DATA-001: only the Evidence Layer writes evidence; the other powers may read it and may not write it.
func TestOnlyEvidenceRwWritesEvidence(t *testing.T) {
	w := newWorld(t)
	if err := insertPayload(w.as(t, "evidence_rw"), ctx(t), tenantA, "pay_1"); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"trust_rw", "execution_rw", "outcome_rw", "orchestrator_rw"} {
		p := w.as(t, role)
		if err := insertPayload(p, ctx(t), tenantA, "pay_"+role); code(err) != denied {
			t.Errorf("%s inserted a payload: err = %v", role, err)
		}
		if err := insertItem(p, ctx(t), tenantA, "evd_"+role, "pay_1", "KNOWN", hashA, 300); code(err) != denied {
			t.Errorf("%s inserted an item: err = %v", role, err)
		}
	}
	for _, role := range []string{"trust_rw", "outcome_rw"} {
		var n int
		if err := w.as(t, role).QueryRow(ctx(t), `SELECT count(*) FROM evidence_item`).Scan(&n); err != nil {
			t.Errorf("%s cannot read evidence_item: %v", role, err)
		}
	}
}
