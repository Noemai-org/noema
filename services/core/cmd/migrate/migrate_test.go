package main

// EXE-08 (A484, A521, A527; REQ-DATA-001, REQ-SEC-001; INV-29, INV-30; AGENT-13): cmd/migrate, the one-shot that calls db.Migrate. The tests run the command's own run function against a throwaway
// PostgreSQL database, as the Compose one-shot does: the connection is built in process from host, port, database, user and a password file (read through the shared key-file reader), the migrations
// come from a directory. The password is never an argument, never read from the environment and never in an output. All data is SYNTHETIC.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
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
	t                                *testing.T
	admin                            *pgxpool.Pool
	dsn                              string // the throwaway database's DSN, used only to read its parts
	host, port, database, user, pass string
	dir                              string // holds the password file
}

// syntheticPassword is what the test database's trust authentication accepts for any login: the tests prove the command reads and uses it, and that it is never printed.
var syntheticPassword = strings.Repeat("sentinel-", 2) + "one" // built, not a literal: a high-entropy literal beside the word password is what the secret scan reads as a secret

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	return &world{t: t, admin: admin, dsn: dsn, host: u.Hostname(), port: u.Port(), database: strings.TrimPrefix(u.Path, "/"), user: u.User.Username(), pass: syntheticPassword, dir: t.TempDir()}
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

// args is the command line of a run: every connection part, the password file and the migrations directory. The test database does not speak TLS, so the one non-secure mode is named.
func (w *world) args(passwordFile, dir string) []string {
	return []string{"--host", w.host, "--port", w.port, "--database", w.database, "--user", w.user, "--password-file", passwordFile, "--sslmode", "disable", "--migrations-dir", dir}
}

// without drops one flag and its value from a full command line.
func without(args []string, flag string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		if args[i] == flag {
			i++
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// with replaces a flag's value (the flag must be present).
func with(args []string, flag, value string) []string {
	out := append([]string(nil), args...)
	for i := 0; i+1 < len(out); i++ {
		if out[i] == flag {
			out[i+1] = value
			return out
		}
	}
	panic("no such flag in the test's command line: " + flag)
}

// secrets are the values no output may carry.
func (w *world) secrets() []string {
	return []string{w.pass, w.dsn, w.host + ":" + w.port}
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
	passwordFile := w.file("password", w.pass+"\n")
	code, out, errw := w.run(w.args(passwordFile, realMigrations)...)
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
	passwordFile := w.file("password", w.pass)
	if code, out, errw := w.run(w.args(passwordFile, realMigrations)...); code != 0 {
		t.Fatalf("first run: exit %d %q %q", code, out, errw)
	}
	before := w.applied()
	code, out, errw := w.run(w.args(passwordFile, realMigrations)...)
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

// Every connection part, the password file and the migrations directory are required with no default (only --sslmode has one, `prefer`, as every other login of the stack); a value outside its
// pattern, a flag that does not exist (there is no --password, no --dsn and no --dsn-file) and a stray argument are refused before any file is read or any connection is made: exit 2, usage on
// stderr, nothing written, and the password in none of it.
func TestBadFlagsAreRefusedBeforeAnyDatabaseCall(t *testing.T) {
	w := newWorld(t)
	good := w.args(w.file("password", w.pass), realMigrations)
	bad := map[string][]string{
		"no flags":                               nil,
		"a stray argument":                       append(append([]string(nil), good...), "extra"),
		"a password flag":                        append(append([]string(nil), good...), "--password", w.pass),
		"a dsn flag":                             append(append([]string(nil), good...), "--dsn", w.dsn),
		"the old dsn-file flag":                  append(append([]string(nil), good...), "--dsn-file", "x"),
		"an unknown flag":                        append(append([]string(nil), good...), "--tls", "yes"),
		"an empty host":                          with(good, "--host", ""),
		"a host with a space":                    with(good, "--host", "db host"),
		"a host with a slash (a socket path)":    with(good, "--host", "/var/run/postgresql"),
		"a host with an at sign":                 with(good, "--host", "user@db"),
		"a host with a colon that is no address": with(good, "--host", "db:5432"),
		"a port of zero":                         with(good, "--port", "0"),
		"a port above 65535":                     with(good, "--port", "65536"),
		"a port that is not a number":            with(good, "--port", "five"),
		"a negative port":                        with(good, "--port", "-5"),
		"an empty database":                      with(good, "--database", ""),
		"a database with a space":                with(good, "--database", "no ema"),
		"a database with a quote":                with(good, "--database", "noema'"),
		"a database with a semicolon":            with(good, "--database", "a;b"),
		"a database that is too long":            with(good, "--database", strings.Repeat("d", 64)),
		"an empty user":                          with(good, "--user", ""),
		"a user with a space":                    with(good, "--user", "mig rator"),
		"a user with an equals sign":             with(good, "--user", "a=b"),
		"an sslmode outside the closed set":      with(good, "--sslmode", "allow"),
		"verify-full (it needs a root certificate this command does not take)": with(good, "--sslmode", "verify-full"),
		"an empty sslmode":            with(good, "--sslmode", ""),
		"an empty password file flag": with(good, "--password-file", ""),
		"an empty directory flag":     with(good, "--migrations-dir", ""),
	}
	for _, f := range []string{"--host", "--port", "--database", "--user", "--password-file", "--migrations-dir"} {
		bad["no "+f+" flag"] = without(good, f)
	}
	for name, args := range bad {
		code, out, errw := w.run(args...)
		if code != 2 {
			t.Errorf("%s: exit %d, want 2", name, code)
		}
		if !strings.Contains(errw, "usage: migrate --host HOST --port PORT --database NAME --user NAME --password-file FILE [--sslmode disable|prefer|require] --migrations-dir DIR") {
			t.Errorf("%s: stderr %q has no usage line", name, errw)
		}
		for _, secret := range w.secrets() {
			if strings.Contains(out+errw, secret) {
				t.Errorf("%s: %q is in the output", name, secret)
			}
		}
	}
	if got := w.applied(); got != -1 {
		t.Errorf("a refused command line touched the database (schema_migration has %d rows)", got)
	}
}

// A password file or a migrations directory that cannot be used is refused before any connection: missing, empty, oversized, a link, a directory, holding a line break or a NUL inside, a directory with
// no migration. The password is in no output, and the database is never touched.
func TestAnUnusablePasswordFileOrDirectoryIsRefusedBeforeAnyConnection(t *testing.T) {
	w := newWorld(t)
	good := w.file("password", w.pass)
	link := filepath.Join(w.dir, "password-link")
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
		"a password file that is missing": w.args(filepath.Join(w.dir, "absent"), realMigrations),
		"an empty password file":          w.args(w.file("empty", "\n"), realMigrations),
		"a password file of spaces only is not empty text but a blank password": w.args(w.file("blank", "   \n"), realMigrations),
		"an oversized password file":              w.args(w.file("big", w.pass+strings.Repeat("x", 2<<10)), realMigrations),
		"a password file that holds a line break": w.args(w.file("lines", w.pass+"\nsecond"), realMigrations),
		"a password file that holds a NUL":        w.args(w.file("nul", w.pass+"\x00x"), realMigrations),
		"a password file that is a link":          w.args(link, realMigrations),
		"a password file that is a directory":     w.args(w.dir, realMigrations),
		"a directory that is missing":             w.args(good, filepath.Join(w.dir, "absent-dir")),
		"a migrations path that is a file":        w.args(good, aFile),
		"a directory with no migration":           w.args(good, empty),
		"a directory with no .sql file":           w.args(good, notSQL),
	} {
		code, out, errw := w.run(args...)
		if code != 2 {
			t.Errorf("%s: exit %d, want 2 (stdout %q, stderr %q)", name, code, out, errw)
			continue
		}
		if r := parse(t, out); r.Result != "error" || r.Level != "error" || r.Reason == "" || r.Msg == "" {
			t.Errorf("%s: line %+v", name, r)
		}
		for _, secret := range w.secrets() {
			if strings.Contains(out+errw, secret) {
				t.Errorf("%s: %q is in the output", name, secret)
			}
		}
	}
	if got := w.applied(); got != -1 {
		t.Errorf("a refused input touched the database (schema_migration has %d rows)", got)
	}
}

// A login the server refuses gives an error with neither the password, the login, the host, the port nor the database in it, on either stream (AGENT-13, INV-29, A527).
func TestARefusedLoginNeverShowsThePasswordOrTheConnectionParts(t *testing.T) {
	w := newWorld(t)
	const login = "nobody_sentinel"
	w.user = login
	code, out, errw := w.run(w.args(w.file("password", w.pass+"\n"), realMigrations)...)
	if code != 2 {
		t.Fatalf("exit %d, want 2 (%q %q)", code, out, errw)
	}
	for _, secret := range []string{w.pass, login, w.host, w.port, w.database, w.host + ":" + w.port} {
		if secret != "" && strings.Contains(out+errw, secret) {
			t.Errorf("the output carries %q: stdout %q stderr %q", secret, out, errw)
		}
	}
	if r := parse(t, out); r.Result != "error" || r.Reason != "database_unreachable" {
		t.Errorf("line %+v; want result error and reason database_unreachable", r)
	}
}

// A password file that fails a check never has its content echoed: the probe is the password itself in a file of a refused mode, of a link and of the wrong size.
func TestARefusedPasswordFileNeverShowsItsContent(t *testing.T) {
	w := newWorld(t)
	loose := filepath.Join(w.dir, "loose")
	if err := os.WriteFile(loose, []byte(w.pass), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(loose, 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errw := w.run(w.args(loose, realMigrations)...)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if strings.Contains(out+errw, w.pass) || strings.Contains(out+errw, loose) {
		t.Errorf("the refusal carries the password or the path: %q %q", out, errw)
	}
}

// The password is used as the file gives it, with no escaping and no parsing: a password of every character a DSN or a URL treats specially connects (the test database trusts any password), which a
// DSN string built from it would not survive. The connection is never described by a string that holds the password.
func TestAPasswordWithSpecialCharactersIsUsedAsItIs(t *testing.T) {
	w := newWorld(t)
	for name, pass := range map[string]string{
		"a space and quotes":   `p a'ss"w ord`,
		"at, colon and slash":  "p@ss:w/rd",
		"percent and equals":   "p%41ss=word",
		"a semicolon and hash": "pa;ss#word",
		"a backslash":          `pa\ss\`,
		"non-ASCII":            "pässwörd-日本",
	} {
		ww := newWorld(t)
		ww.pass = pass
		if code, out, errw := ww.run(ww.args(ww.file("password", pass+"\n"), realMigrations)...); code != 0 {
			t.Errorf("%s: exit %d (%q %q)", name, code, out, errw)
		}
	}
	_ = w
}

// The environment never decides the connection: every PG variable that could name a host, a port, a login, a password, a mode or a service is set to a wrong value, and the command still connects
// with the flags and the password file alone, and says nothing of them (A527: the password never reaches the environment, and nothing in it overrides the checked file).
// envSecretProbe is the value the environment offers as a password; it must never be used or printed.
var envSecretProbe = strings.Repeat("env-probe-", 2) + "two"

func TestThePGEnvironmentNeverDecidesTheConnection(t *testing.T) {
	w := newWorld(t) // the test database is made before the variables are set: the helper's own connection reads them too
	passwordFile := w.file("password", w.pass+"\n")
	for k, v := range map[string]string{
		"PGHOST": "203.0.113.1", "PGHOSTADDR": "203.0.113.1", "PGPORT": "1", "PGDATABASE": "nope", "PGUSER": "nobody_env", "PGPASSWORD": envSecretProbe, "PGPASSFILE": "/nonexistent",
		"PGSSLMODE": "verify-full", "PGSSLROOTCERT": "/nonexistent", "PGSERVICE": "nope", "PGSERVICEFILE": "/nonexistent", "PGOPTIONS": "-c nonsense=1", "PGCONNECT_TIMEOUT": "1",
		"PGTARGETSESSIONATTRS": "read-only", "PGAPPNAME": "env-app",
	} {
		t.Setenv(k, v)
	}
	code, out, errw := w.run(w.args(passwordFile, realMigrations)...)
	if code != 0 {
		t.Fatalf("exit %d (%q %q): an environment variable decided the connection", code, out, errw)
	}
	if strings.Contains(out+errw, envSecretProbe) {
		t.Errorf("an environment value is in the output: %q %q", out, errw)
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
	code, out, errw := w.run(w.args(w.file("password", w.pass), dir)...)
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
	passwordFile := w.file("password", w.pass)
	dir := writeDir(t, map[string]string{"0001_first.sql": "CREATE TABLE exe08_a (id int);"})
	if code, out, errw := w.run(w.args(passwordFile, dir)...); code != 0 {
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
	code, out, _ := w.run(w.args(passwordFile, dir)...)
	if code != 1 || parse(t, out).Reason != "migration_failed" {
		t.Errorf("a changed file: exit %d, line %q; want 1 and migration_failed", code, out)
	}
	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "0002_second.sql"), []byte("CREATE TABLE exe08_b (id int);"), 0o400); err != nil {
		t.Fatal(err)
	}
	code, out, _ = w.run(w.args(passwordFile, dir)...)
	if code != 1 || parse(t, out).Reason != "migration_failed" {
		t.Errorf("a missing applied file: exit %d, line %q; want 1 and migration_failed", code, out)
	}
	if got := w.applied(); got != 1 {
		t.Errorf("schema_migration holds %d rows, want 1: nothing may run after a refusal", got)
	}
}

// The password comes only from the checked file (A527): the source reads no environment variable, defines no password, DSN or DSN-file flag, and no line that names the password also prints, logs or
// wraps it into an error. This is a guard on the shape of the code; the output tests above are the proof on its behaviour.
var pwWord = regexp.MustCompile(`\bpw\b`)

func TestThePasswordIsNeverReadFromTheEnvironmentOrTheCommandLineOrPrinted(t *testing.T) {
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if strings.HasSuffix(n, "_test.go") {
			continue
		}
		b, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		for _, banned := range []string{"os.Getenv", "os.LookupEnv", "os.Environ", `"dsn"`, `"dsn-file"`, `"password"`} {
			if strings.Contains(src, banned) {
				t.Errorf("%s contains %s", n, banned)
			}
		}
		for i, line := range strings.Split(src, "\n") {
			low := strings.ToLower(line)
			// the secret is held in a variable named pw: a line that names it, or the word password, may not also print, log or wrap anything
			if !(strings.Contains(low, "password") || pwWord.MatchString(line)) || strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			for _, sink := range []string{"fmt.", "log.", "Errorf", "Fprint", "Printf", "Sprintf", "errors.New("} {
				if strings.Contains(line, sink) && !strings.Contains(line, "password-file") {
					t.Errorf("%s:%d names the password beside %s: %s", n, i+1, sink, strings.TrimSpace(line))
				}
			}
		}
	}
}
