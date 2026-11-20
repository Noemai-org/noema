package main

// EXE-08 (A484; REQ-DATA-001; INV-30): cmd/migrate, the one-shot that calls db.Migrate. The tests run the command's own run function against a throwaway PostgreSQL database,
// as the Compose one-shot does: the DSN comes from a file, the migrations from a directory. All data is SYNTHETIC.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

func TestMain(m *testing.M) { testpg.Main(m) }

var epoch = time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)

const realMigrations = "../../migrations"

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

type result struct {
	Ts, Level, Service, Power, Event, Msg, Result, Reason, SQLState string
	CorrelationID                                                   string `json:"correlation_id"`
	AppliedCount                                                    int    `json:"applied_count"`
	Applied                                                         []string
}

type world struct {
	t     *testing.T
	admin *pgxpool.Pool
	dsn   string
	dir   string // holds the DSN file
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	return &world{t: t, admin: admin, dsn: dsn, dir: t.TempDir()}
}

// file writes a file of the world's directory and returns its path.
func (w *world) file(name, body string) string {
	w.t.Helper()
	p := filepath.Join(w.dir, name)
	if err := os.WriteFile(p, []byte(body), 0o400); err != nil {
		w.t.Fatal(err)
	}
	return p
}

// run runs the command and returns its exit code, stdout and stderr.
func (w *world) run(args ...string) (int, string, string) {
	w.t.Helper()
	var out, errw bytes.Buffer
	code := run(args, &out, &errw, clock.NewManual(epoch))
	return code, out.String(), errw.String()
}

func (w *world) args(dsnFile, dir string) []string {
	return []string{"--dsn-file", dsnFile, "--migrations-dir", dir}
}

func parse(t *testing.T, out string) result {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("want one JSON line on stdout, got %d: %q", len(lines), out)
	}
	var r result
	if err := json.Unmarshal([]byte(lines[0]), &r); err != nil {
		t.Fatalf("stdout is not one JSON line: %v: %q", err, out)
	}
	return r
}

// applied counts the rows of schema_migration, or -1 when the table does not exist: the command never touched the database.
func (w *world) applied() int {
	w.t.Helper()
	var n int
	if err := w.admin.QueryRow(ctx(w.t), `SELECT count(*) FROM schema_migration`).Scan(&n); err != nil {
		return -1
	}
	return n
}

func sqlFiles(t *testing.T, dir string) []string {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no migrations in %s (%v)", dir, err)
	}
	return names
}

// A fresh database gets every migration, in order, and the line names how many and which (REQ-DATA-001).
func TestRunAppliesEveryMigrationToAFreshDatabase(t *testing.T) {
	w := newWorld(t)
	dsnFile := w.file("dsn", w.dsn+"\n")
	code, out, errw := w.run(w.args(dsnFile, realMigrations)...)
	if code != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errw)
	}
	files := sqlFiles(t, realMigrations)
	r := parse(t, out)
	if r.Result != "migrated" || r.AppliedCount != len(files) || len(r.Applied) != len(files) || r.Level != "info" || r.Service != "migrate" || r.Event != "migrate" {
		t.Errorf("line %+v; want result migrated and %d applied", r, len(files))
	}
	if r.Applied[0] != filepath.Base(files[0]) || r.Applied[len(r.Applied)-1] != filepath.Base(files[len(files)-1]) {
		t.Errorf("applied %v; want file-name order from %s to %s", r.Applied, filepath.Base(files[0]), filepath.Base(files[len(files)-1]))
	}
	if r.CorrelationID == "" || r.Ts != epoch.Format(time.RFC3339) {
		t.Errorf("the PERD 44.2 fields are missing: %+v", r)
	}
	if got := w.applied(); got != len(files) {
		t.Errorf("schema_migration holds %d rows, want %d", got, len(files))
	}
}

// A second run on the same database applies nothing and says so; it is an exit 0, not a failure (a Compose one-shot runs on every start).
func TestASecondRunAppliesNothing(t *testing.T) {
	w := newWorld(t)
	dsnFile := w.file("dsn", w.dsn)
	if code, out, errw := w.run(w.args(dsnFile, realMigrations)...); code != 0 {
		t.Fatalf("first run: exit %d %q %q", code, out, errw)
	}
	before := w.applied()
	code, out, errw := w.run(w.args(dsnFile, realMigrations)...)
	if code != 0 {
		t.Fatalf("second run: exit %d %q %q", code, out, errw)
	}
	r := parse(t, out)
	if r.Result != "up_to_date" || r.AppliedCount != 0 || len(r.Applied) != 0 {
		t.Errorf("second run line %+v; want up_to_date and nothing applied", r)
	}
	if w.applied() != before {
		t.Error("the second run changed schema_migration")
	}
}

// Both flags are required, with no default, and a bad command line reaches no database: the valid DSN file is never used (exit 2, nothing written).
func TestBadFlagsAreRefusedBeforeAnyDatabaseCall(t *testing.T) {
	w := newWorld(t)
	dsnFile := w.file("dsn", w.dsn)
	for name, args := range map[string][]string{
		"no flags":                nil,
		"no migrations dir":       {"--dsn-file", dsnFile},
		"no dsn file":             {"--migrations-dir", realMigrations},
		"an empty dsn flag":       {"--dsn-file", "", "--migrations-dir", realMigrations},
		"an empty directory flag": {"--dsn-file", dsnFile, "--migrations-dir", ""},
		"an unknown flag":         {"--dsn-file", dsnFile, "--migrations-dir", realMigrations, "--dsn", w.dsn},
		"a stray argument":        {"--dsn-file", dsnFile, "--migrations-dir", realMigrations, "extra"},
	} {
		code, out, errw := w.run(args...)
		if code != 2 {
			t.Errorf("%s: exit %d, want 2", name, code)
		}
		if !strings.Contains(errw, "usage: migrate --dsn-file FILE --migrations-dir DIR") {
			t.Errorf("%s: stderr %q has no usage line", name, errw)
		}
		if strings.Contains(out+errw, w.dsn) {
			t.Errorf("%s: the DSN is in the output", name)
		}
	}
	if got := w.applied(); got != -1 {
		t.Errorf("a refused command line touched the database (schema_migration has %d rows)", got)
	}
}

// A DSN file or a migrations directory that cannot be used is refused before any connection: missing, empty, oversized, a link, a directory, a directory with no migration.
func TestAnUnusableDSNFileOrDirectoryIsRefusedBeforeAnyConnection(t *testing.T) {
	w := newWorld(t)
	good := w.file("dsn", w.dsn)
	link := filepath.Join(w.dir, "dsn-link")
	if err := os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}
	empty := t.TempDir()
	notSQL := t.TempDir()
	if err := os.WriteFile(filepath.Join(notSQL, "notes.txt"), []byte("x"), 0o400); err != nil {
		t.Fatal(err)
	}
	aFile := w.file("a-file", "x")
	for name, args := range map[string][]string{
		"a DSN file that is missing":       w.args(filepath.Join(w.dir, "absent"), realMigrations),
		"an empty DSN file":                w.args(w.file("empty", " \n"), realMigrations),
		"an oversized DSN file":            w.args(w.file("big", w.dsn+strings.Repeat(" ", 9<<10)), realMigrations),
		"a DSN file that is a link":        w.args(link, realMigrations),
		"a DSN file that is a directory":   w.args(w.dir, realMigrations),
		"a directory that is missing":      w.args(good, filepath.Join(w.dir, "absent-dir")),
		"a migrations path that is a file": w.args(good, aFile),
		"a directory with no migration":    w.args(good, empty),
		"a directory with no .sql file":    w.args(good, notSQL),
		"a DSN that does not parse":        w.args(w.file("junk", "not a dsn at all ://"), realMigrations),
	} {
		code, out, errw := w.run(args...)
		if code != 2 {
			t.Errorf("%s: exit %d, want 2 (stdout %q, stderr %q)", name, code, out, errw)
			continue
		}
		if r := parse(t, out); r.Result != "error" || r.Level != "error" || r.Reason == "" || r.Msg == "" {
			t.Errorf("%s: line %+v", name, r)
		}
		if strings.Contains(out+errw, w.dsn) {
			t.Errorf("%s: the DSN is in the output", name)
		}
	}
	if got := w.applied(); got != -1 {
		t.Errorf("a refused input touched the database (schema_migration has %d rows)", got)
	}
}

// A DSN the server refuses gives an error with neither the password, the login, the host nor the DSN in it, on either stream (AGENT-13, INV-29).
func TestARefusedLoginNeverShowsTheDSNOrItsParts(t *testing.T) {
	w := newWorld(t)
	u, err := url.Parse(w.dsn)
	if err != nil {
		t.Fatal(err)
	}
	const password, login = "p4ss-SENTINEL-9f3a", "nobody_sentinel"
	u.User = url.UserPassword(login, password)
	bad := u.String()
	code, out, errw := w.run(w.args(w.file("dsn", bad), realMigrations)...)
	if code != 2 {
		t.Fatalf("exit %d, want 2 (%q %q)", code, out, errw)
	}
	for _, secret := range []string{password, login, bad, u.Host, u.Hostname(), u.Port(), strings.TrimPrefix(u.Path, "/")} {
		if secret != "" && strings.Contains(out+errw, secret) {
			t.Errorf("the output carries %q: stdout %q stderr %q", secret, out, errw)
		}
	}
	if r := parse(t, out); r.Result != "error" || r.Reason != "database_unreachable" {
		t.Errorf("line %+v; want result error and reason database_unreachable", r)
	}
}

func writeDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for n, b := range files {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(b), 0o400); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// A migration that fails stops the run with exit 1: the files before it stay applied and are named, the failing file is not recorded, and the line carries the SQLSTATE and no SQL or driver text.
func TestAFailingMigrationStopsTheRunAndNamesWhatWasApplied(t *testing.T) {
	w := newWorld(t)
	dir := writeDir(t, map[string]string{
		"0001_first.sql":  "CREATE TABLE exe08_first (id int);",
		"0002_broken.sql": "CREATE TABLE exe08_secret_marker (id int, nonsense);",
		"0003_never.sql":  "CREATE TABLE exe08_never (id int);",
	})
	code, out, errw := w.run(w.args(w.file("dsn", w.dsn), dir)...)
	if code != 1 {
		t.Fatalf("exit %d, want 1 (%q %q)", code, out, errw)
	}
	r := parse(t, out)
	if r.Result != "refused" || r.Reason != "migration_failed" || r.SQLState == "" || len(r.SQLState) != 5 || r.AppliedCount != 1 || len(r.Applied) != 1 || r.Applied[0] != "0001_first.sql" {
		t.Errorf("line %+v", r)
	}
	if strings.Contains(out+errw, "exe08_secret_marker") || strings.Contains(out+errw, "nonsense") {
		t.Errorf("SQL or driver text leaked: %q %q", out, errw)
	}
	if got := w.applied(); got != 1 {
		t.Errorf("schema_migration holds %d rows, want 1 (the failing file is not recorded, the later one never runs)", got)
	}
}

// Forward-only: a file that changed after it was applied, and an applied file that is gone, are refused with exit 1 and nothing runs.
func TestAChangedOrMissingAppliedMigrationIsRefused(t *testing.T) {
	w := newWorld(t)
	dsnFile := w.file("dsn", w.dsn)
	dir := writeDir(t, map[string]string{"0001_first.sql": "CREATE TABLE exe08_a (id int);"})
	if code, out, errw := w.run(w.args(dsnFile, dir)...); code != 0 {
		t.Fatalf("setup: exit %d %q %q", code, out, errw)
	}
	chmod := func(p string) {
		if err := os.Chmod(p, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	first := filepath.Join(dir, "0001_first.sql")
	chmod(first)
	if err := os.WriteFile(first, []byte("CREATE TABLE exe08_a (id int, more int);"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, _ := w.run(w.args(dsnFile, dir)...)
	if code != 1 || parse(t, out).Reason != "migration_failed" {
		t.Errorf("a changed file: exit %d, line %q; want 1 and migration_failed", code, out)
	}
	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "0002_second.sql"), []byte("CREATE TABLE exe08_b (id int);"), 0o400); err != nil {
		t.Fatal(err)
	}
	code, out, _ = w.run(w.args(dsnFile, dir)...)
	if code != 1 || parse(t, out).Reason != "migration_failed" {
		t.Errorf("a missing applied file: exit %d, line %q; want 1 and migration_failed", code, out)
	}
	if got := w.applied(); got != 1 {
		t.Errorf("schema_migration holds %d rows, want 1: nothing may run after a refusal", got)
	}
}

// The DSN comes only from the mounted file: the source reads no environment variable and the command line has no DSN flag (A484; AGENT-13).
func TestTheDSNIsNeverReadFromTheEnvironmentOrTheCommandLine(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"os.Getenv", "os.LookupEnv", "os.Environ", `"dsn"`} {
		if strings.Contains(string(src), banned) {
			t.Errorf("main.go contains %s", banned)
		}
	}
}
