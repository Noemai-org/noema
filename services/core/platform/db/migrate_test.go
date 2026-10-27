package db_test

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/db"
)

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return c
}

// REQ-DATA-001: migrations are forward-only. The first run applies every file; the second applies none.
func TestMigrateAppliesOnceAndIsRepeatable(t *testing.T) {
	pool, _ := testpg.NewDatabase(t)
	first, err := db.Migrate(ctx(t), pool, migrations.FS)
	if err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if len(first) == 0 || first[0] != "0001_execution_and_record.sql" {
		t.Fatalf("applied %v, want 0001_execution_and_record.sql first", first)
	}
	second, err := db.Migrate(ctx(t), pool, migrations.FS)
	if err != nil || len(second) != 0 {
		t.Fatalf("second migrate applied %v, err %v; want nothing", second, err)
	}
}

// AGENTS.md section 7: "never edited after merge". A changed file, a removed file and a badly named file are errors.
func TestMigrateRefusesEditedRemovedAndMisnamedFiles(t *testing.T) {
	pool, _ := testpg.NewDatabase(t)
	one := fstest.MapFS{"0001_a.sql": {Data: []byte("CREATE TABLE a (x int);")}}
	if _, err := db.Migrate(ctx(t), pool, one); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	edited := fstest.MapFS{"0001_a.sql": {Data: []byte("CREATE TABLE a (x int, y int);")}}
	if _, err := db.Migrate(ctx(t), pool, edited); err == nil || !strings.Contains(err.Error(), "changed after it was applied") {
		t.Fatalf("an edited migration: err %v, want a refusal", err)
	}
	if _, err := db.Migrate(ctx(t), pool, fstest.MapFS{"0002_b.sql": {Data: []byte("SELECT 1;")}}); err == nil || !strings.Contains(err.Error(), "forward-only") {
		t.Fatalf("a removed migration: err %v, want a refusal", err)
	}
	if _, err := db.Migrate(ctx(t), pool, fstest.MapFS{"0001_a.sql": one["0001_a.sql"], "b.sql": {Data: []byte("SELECT 1;")}}); err == nil || !strings.Contains(err.Error(), "NNNN_name.sql") {
		t.Fatalf("a misnamed migration: err %v, want a refusal", err)
	}
}

// A migration that fails leaves no trace: its statements and its schema_migration row roll back together.
func TestMigrateFailureRollsBackThatFile(t *testing.T) {
	pool, _ := testpg.NewDatabase(t)
	bad := fstest.MapFS{"0001_bad.sql": {Data: []byte("CREATE TABLE half (x int); SELECT 1/0;")}}
	if _, err := db.Migrate(ctx(t), pool, bad); err == nil {
		t.Fatal("a failing migration returned no error")
	}
	var n int
	if err := pool.QueryRow(ctx(t), `SELECT count(*) FROM pg_tables WHERE tablename = 'half'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("table half exists after a failed migration (count %d, err %v)", n, err)
	}
	if err := pool.QueryRow(ctx(t), `SELECT count(*) FROM schema_migration`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("schema_migration has %d rows after a failed migration (err %v)", n, err)
	}
}

func TestMigrateNeedsAContextDeadline(t *testing.T) {
	pool, _ := testpg.NewDatabase(t)
	if _, err := db.Migrate(context.Background(), pool, migrations.FS); err == nil {
		t.Fatal("Migrate accepted a context without a deadline")
	}
}
