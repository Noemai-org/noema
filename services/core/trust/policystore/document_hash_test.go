package policystore_test

// TRU-02 (REQ-TRU-005, REQ-TRU-004, REQ-SEC-004; INV-09, INV-21; ruling on noema#771, interim): the hash of the policy document is part of a publication. The publication row keeps
// document_hash; the database requires it for every NEW publication (a trigger), checks its form (a CHECK), and leaves the rows published before it NULL, because a publication is
// insert-only and the documents themselves are not stored (DEBT-23). The tests run on real PostgreSQL with the real migrations; the legacy rows are made by migrating up to the number
// before the hash migration, inserting, and then migrating the rest. All data is SYNTHETIC.

import (
	"errors"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/recordsig"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/policystore"
	"math/rand"
)

// documentHashMigration is the number of the migration that adds document_hash.
const documentHashMigration = 35

const otherDocHash = "sha256:11111111111111111111111111111111111111111111111111111111111111bb"

func storedHash(t *testing.T, w *world, tenant, policyID, version string) (hash *string) {
	t.Helper()
	if err := w.admin.QueryRow(ctx(t), `SELECT document_hash FROM policy_publication WHERE tenant_id = $1 AND policy_id = $2 AND version = $3`, tenant, policyID, version).Scan(&hash); err != nil {
		t.Fatalf("read the stored hash: %v", err)
	}
	return hash
}

func publicationCount(t *testing.T, w *world) int {
	t.Helper()
	var n int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM policy_publication`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// REQ-TRU-005: a publication stores the document's hash, byte for byte.
func TestAPublicationStoresTheDocumentHash(t *testing.T) {
	w := newWorld(t)
	if err := w.publishHashed(t, w.trustA, tenantA, "fin.pol", "3.1", otherDocHash); err != nil {
		t.Fatal(err)
	}
	if got := storedHash(t, w, tenantA, "fin.pol", "3.1"); got == nil || *got != otherDocHash {
		t.Fatalf("stored hash %v, want %s", got, otherDocHash)
	}
}

// REQ-TRU-005: a request with no hash, or a hash outside the form, is refused before anything is written (ErrRequest, fixed text), and nothing reaches the Record.
func TestARequestWithAMissingOrMalformedHashIsRefusedAndWritesNothing(t *testing.T) {
	w := newWorld(t)
	for name, h := range map[string]string{
		"empty": "", "no prefix": strings.Repeat("a", 64), "upper-case hex": "sha256:" + strings.Repeat("A", 64), "short": "sha256:" + strings.Repeat("a", 63),
		"long": "sha256:" + strings.Repeat("a", 65), "another algorithm": "sha512:" + strings.Repeat("a", 64), "not hex": "sha256:" + strings.Repeat("g", 64),
		"trailing newline": "sha256:" + strings.Repeat("a", 64) + "\n", "leading space": " sha256:" + strings.Repeat("a", 64),
	} {
		err := w.publishHashed(t, w.trustA, tenantA, "fin.pol", "3.1", h)
		if !errors.Is(err, policystore.ErrRequest) {
			t.Errorf("%s: err %v, want ErrRequest", name, err)
			continue
		}
		if strings.Contains(err.Error(), h) && h != "" {
			t.Errorf("%s: the error text repeats the input", name)
		}
	}
	if n := publicationCount(t, w); n != 0 {
		t.Fatalf("%d publications after refused requests", n)
	}
	var events int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM record_event WHERE event_type = 'POLICY_PUBLISHED'`).Scan(&events); err != nil || events != 0 {
		t.Fatalf("POLICY_PUBLISHED events %d (%v), want 0", events, err)
	}
}

func pgCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code + " " + pe.ConstraintName
	}
	return ""
}

// REQ-TRU-005, INV-09: the database requires the hash even when the caller is not this package: a direct INSERT as the tenant's own Trust login with no hash is refused (the trigger),
// and one with a hash outside the form is refused (the CHECK).
func TestTheDatabaseRefusesAPublicationWithNoHashOrAMalformedOne(t *testing.T) {
	w := newWorld(t)
	insert := func(hash any) error {
		_, err := w.trustA.Exec(ctx(t), `INSERT INTO policy_publication (tenant_id, policy_id, version, version_key, published_by, document_hash)
			VALUES ($1, 'fin.pol', '1.0', policy_version_key('1.0'), 'adm_synth01', $2)`, tenantA, hash)
		return err
	}
	err := insert(nil)
	if err == nil || !strings.HasPrefix(pgCode(err), "23502") {
		t.Fatalf("a NULL hash: err %v (%s), want SQLSTATE 23502 from the trigger", err, pgCode(err))
	}
	if _, err := w.trustA.Exec(ctx(t), `INSERT INTO policy_publication (tenant_id, policy_id, version, version_key, published_by)
		VALUES ($1, 'fin.pol', '1.0', policy_version_key('1.0'), 'adm_synth01')`, tenantA); err == nil || !strings.HasPrefix(pgCode(err), "23502") {
		t.Fatalf("no hash column in the INSERT: err %v (%s), want SQLSTATE 23502 from the trigger", err, pgCode(err))
	}
	for name, h := range map[string]string{"upper-case": "sha256:" + strings.Repeat("A", 64), "short": "sha256:abc", "no prefix": strings.Repeat("a", 64), "empty": "", "newline": "sha256:" + strings.Repeat("a", 64) + "\n"} {
		err := insert(h)
		if err == nil || pgCode(err) != "23514 policy_publication_document_hash_form" {
			t.Errorf("%s: err %v (%s), want SQLSTATE 23514 from policy_publication_document_hash_form", name, err, pgCode(err))
		}
	}
	if n := publicationCount(t, w); n != 0 {
		t.Fatalf("%d publications written by refused inserts", n)
	}
	if err := insert("sha256:" + strings.Repeat("a", 64)); err != nil {
		t.Fatalf("a well-formed hash through the same INSERT: %v", err)
	}
}

// REQ-SEC-004: a tenant's login reads its own publication hashes and no other tenant's (the row-level security of 0015 covers the new column).
func TestADocumentHashIsReadableOnlyByItsOwnTenant(t *testing.T) {
	w := newWorld(t)
	if err := w.publishHashed(t, w.trustA, tenantA, "fin.pol", "3.1", otherDocHash); err != nil {
		t.Fatal(err)
	}
	if err := w.publishHashed(t, w.trustB, tenantB, "fin.pol", "3.1", testDocHash); err != nil {
		t.Fatal(err)
	}
	read := func(p *pgxpool.Pool) []string {
		rows, err := p.Query(ctx(t), `SELECT document_hash FROM policy_publication ORDER BY tenant_id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var h string
			if err := rows.Scan(&h); err != nil {
				t.Fatal(err)
			}
			out = append(out, h)
		}
		return out
	}
	if a, b := read(w.trustA), read(w.trustB); len(a) != 1 || a[0] != otherDocHash || len(b) != 1 || b[0] != testDocHash {
		t.Fatalf("tenant A reads %v, tenant B reads %v", a, b)
	}
	if got := read(w.execA); len(got) != 1 || got[0] != otherDocHash {
		t.Fatalf("tenant A's Execution login reads %v", got)
	}
}

var migrationNumber = regexp.MustCompile(`^([0-9]{4})_`)

// migrationsBefore is the migrations with a number below n.
func migrationsBefore(t *testing.T, n int) fs.FS {
	t.Helper()
	out := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		m := migrationNumber.FindStringSubmatch(e.Name())
		if m == nil || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		if k, _ := strconv.Atoi(m[1]); k < n {
			b, err := fs.ReadFile(migrations.FS, e.Name())
			if err != nil {
				t.Fatal(err)
			}
			out[e.Name()] = &fstest.MapFile{Data: b}
		}
	}
	return out
}

// REQ-TRU-005, INV-21: a publication made before the hash existed keeps a NULL hash, stays readable as the active version, cannot be edited into having one (a publication is insert-only),
// and does not stop a newer version from being published with its hash.
func TestAPublicationMadeBeforeTheHashKeepsANullHashAndStaysReadable(t *testing.T) {
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrationsBefore(t, documentHashMigration)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{tenantA, tenantB} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'T')`, id); err != nil {
			t.Fatal(err)
		}
	}
	trustA := testpg.TenantLogin(t, admin, dsn, "trust_rw", tenantA)
	execA := testpg.TenantLogin(t, admin, dsn, "execution_rw", tenantA)
	if _, err := trustA.Exec(ctx(t), `INSERT INTO policy_publication (tenant_id, policy_id, version, version_key, published_by)
		VALUES ($1, 'fin.pol', '3.1', policy_version_key('3.1'), 'adm_synth01')`, tenantA); err != nil {
		t.Fatalf("a legacy publication before the hash migration: %v", err)
	}
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewManual(epoch)
	w := &world{admin: admin, trustA: trustA, execA: execA, trustB: testpg.TenantLogin(t, admin, dsn, "trust_rw", tenantB),
		store: &policystore.Store{Appender: &chain.Appender{Clock: clk, IDs: ids.New(clk, rand.New(rand.NewSource(1))), Signers: recordsig.For(t, admin, "policystore", nil, "core-trust")}}}
	if got := storedHash(t, w, tenantA, "fin.pol", "3.1"); got != nil {
		t.Fatalf("the legacy row has hash %v, want NULL", *got)
	}
	if v := active(t, execA, tenantA, "fin.pol"); v != "fin.pol@3.1" {
		t.Fatalf("active %q after the migration, want fin.pol@3.1", v)
	}
	if _, err := admin.Exec(ctx(t), `UPDATE policy_publication SET document_hash = $1`, otherDocHash); err == nil {
		t.Fatal("a publication was edited into having a hash")
	}
	if err := w.publishHashed(t, trustA, tenantA, "fin.pol", "3.2", otherDocHash); err != nil {
		t.Fatalf("a newer version over a legacy one: %v", err)
	}
	if got := storedHash(t, w, tenantA, "fin.pol", "3.2"); got == nil || *got != otherDocHash {
		t.Fatalf("the new row's hash %v, want %s", got, otherDocHash)
	}
	if got := storedHash(t, w, tenantA, "fin.pol", "3.1"); got != nil {
		t.Fatal("the legacy row gained a hash")
	}
}
