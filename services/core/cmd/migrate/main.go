// Command migrate applies the database migrations once and exits (EXE-08; A484, A521, A527; REQ-DATA-001, REQ-SEC-001; INV-29, INV-30; AGENT-13). See README.md.
//
//	migrate --host HOST --port PORT --database NAME --user NAME --password-file FILE [--sslmode disable|prefer|require] --migrations-dir DIR
//
// The connection is built in process from its parts. The migration login's password comes only from the file, read through the shared key-file reader (mode 0400, owned by the user the command runs as,
// no link, one descriptor): it is never an argument, never read from the environment, never part of a string the command builds, and never printed. The migrations are the .sql files of the directory,
// applied by platform/db.Migrate in file-name order, each in its own transaction, forward-only.
//
// Output: one JSON line on stdout (PERD 44.2 fields, never the password, a login, a host, a SQL text or a driver's text) and, when the exit code is not 0, one fixed sentence on stderr.
// Exit codes: 0 migrated, or already up to date; 1 a migration was refused or failed (a changed or missing applied file, a statement the database refused); 2 the command could not run
// (a bad argument, a secret file or a directory that cannot be used, a database that cannot be reached).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/trust/grants/keyfiles"
)

const (
	exitDone    = 0
	exitRefused = 1
	exitCannot  = 2

	// runTimeout bounds the whole run: db.Migrate refuses a context without a deadline. An operating limit of this command, not a safety threshold.
	runTimeout = 5 * time.Minute

	// maxSecretBytes is the most the secret file may hold: a password and a final newline.
	maxSecretBytes = 1 << 10

	// connectTimeout bounds one connection attempt, so that nothing in the environment decides it.
	connectTimeout = 30 * time.Second

	usage = "usage: migrate --host HOST --port PORT --database NAME --user NAME --password-file FILE [--sslmode disable|prefer|require] --migrations-dir DIR"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, clock.System{})) }

// settings are the parts of the connection that are not secret, the file the secret is in, and the migrations directory.
type settings struct{ host, port, database, user, secretFile, sslMode, migrationsDir string }

var (
	// hostRe is one DNS name: labels of letters, digits and hyphens. An IP literal is checked apart.
	hostRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$`)
	portRe = regexp.MustCompile(`^[0-9]{1,5}$`)
	// nameRe is a database or a login name: conservative, so that no value can carry a space, a quote or an equals sign into the parsed settings.
	nameRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,62}$`)
)

// sslModes is the closed set of transport modes. `prefer` is the driver's default and the one every other login of the stack uses (their DSNs name none); `disable` is for a network with no TLS;
// `require` insists on TLS. `verify-full` is not offered: it needs a root certificate this command does not take.
var sslModes = map[string]bool{"disable": true, "prefer": true, "require": true}

func parseArgs(args []string) (settings, bool) {
	var s settings
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&s.host, "host", "", "the database host: a DNS name or an IP address (no default)")
	fs.StringVar(&s.port, "port", "", "the database port, 1 to 65535 (no default)")
	fs.StringVar(&s.database, "database", "", "the database name (no default)")
	fs.StringVar(&s.user, "user", "", "the migration login (no default)")
	fs.StringVar(&s.secretFile, "password-file", "", "the file holding the login's password: mode 0400, owned by the user the command runs as (no default)")
	fs.StringVar(&s.sslMode, "sslmode", "prefer", "the transport mode: disable, prefer or require")
	fs.StringVar(&s.migrationsDir, "migrations-dir", "", "the directory of the .sql migrations (no default)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return s, false
	}
	if s.secretFile == "" || s.migrationsDir == "" || !sslModes[s.sslMode] || !nameRe.MatchString(s.database) || !nameRe.MatchString(s.user) || !validHost(s.host) || !validPort(s.port) {
		return s, false
	}
	return s, true
}

func validHost(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	return net.ParseIP(h) != nil || hostRe.MatchString(h)
}

func validPort(p string) bool {
	if !portRe.MatchString(p) {
		return false
	}
	n, err := strconv.Atoi(p)
	return err == nil && n >= 1 && n <= 65535
}

// Typed reasons a run ends. The sentence for each is fixed; no error's text is ever printed.
var (
	errSecretFile   = errors.New("migrate: the secret file cannot be used")
	errConfig       = errors.New("migrate: the connection cannot be set up")
	errMigrationDir = errors.New("migrate: the migrations directory cannot be used")
	errUnreachable  = errors.New("migrate: the database cannot be reached")
	errMigration    = errors.New("migrate: a migration was refused or failed")
)

// logLine is the one line printed on stdout: the PERD 44.2 fields and the result, enums, counts and file names only.
type logLine struct {
	Ts            string   `json:"ts"`
	Level         string   `json:"level"`
	Service       string   `json:"service"`
	Power         string   `json:"power"`
	Event         string   `json:"event"`
	CorrelationID string   `json:"correlation_id,omitempty"`
	Msg           string   `json:"msg"`
	Result        string   `json:"result"`
	Reason        string   `json:"reason,omitempty"`
	AppliedCount  int      `json:"applied_count"`
	Applied       []string `json:"applied"`
	SQLState      string   `json:"sqlstate,omitempty"`
}

// failure maps an error to the exit code, the reason enum and the fixed sentence.
func failure(err error) (code int, reason, sentence string) {
	switch {
	case errors.Is(err, errSecretFile):
		return exitCannot, "password_file_unusable", "the password file cannot be used"
	case errors.Is(err, errConfig):
		return exitCannot, "connection_unusable", "the connection cannot be set up"
	case errors.Is(err, errMigrationDir):
		return exitCannot, "migrations_dir_unusable", "the migrations directory cannot be used"
	case errors.Is(err, errUnreachable):
		return exitCannot, "database_unreachable", "the database cannot be reached"
	case errors.Is(err, errMigration):
		return exitRefused, "migration_failed", "a migration was refused or failed; the ones before it stay applied"
	}
	return exitCannot, "error", "the migration could not be completed"
}

// sqlState is the five-character SQLSTATE of a database error, the one part of it that is a fixed code.
func sqlState(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && len(pg.Code) == 5 {
		return pg.Code
	}
	return ""
}

// stateError keeps the SQLSTATE of a refusal and nothing else: a driver's text can quote the DSN or a statement.
type stateError struct {
	kind  error
	state string
}

func (e stateError) Error() string { return e.kind.Error() }
func (e stateError) Unwrap() error { return e.kind }

// say writes one diagnostic line to errw. The exit code and the result line carry the outcome, so a failed write to errw is reported nowhere else (it is the last channel).
func say(errw io.Writer, line string) { _, _ = fmt.Fprintln(errw, line) }

func run(args []string, out, errw io.Writer, clk clock.Clock) int {
	s, ok := parseArgs(args)
	if !ok {
		say(errw, "migrate: "+usage)
		return exitCannot
	}
	line := logLine{Service: "migrate", Power: "migration", Event: "migrate", Applied: []string{}}
	finish := func(err error) int {
		line.Ts = clk.Now().Format(time.RFC3339)
		code := exitDone
		if err == nil {
			line.Level, line.Result, line.Msg = "info", "migrated", "the migrations were applied"
			if line.AppliedCount == 0 {
				line.Result, line.Msg = "up_to_date", "the database is up to date; nothing was applied"
			}
		} else {
			var sentence string
			code, line.Reason, sentence = failure(err)
			var se stateError
			if errors.As(err, &se) {
				line.SQLState = se.state
			}
			line.Level, line.Result, line.Msg = "error", "error", sentence
			if code == exitRefused {
				line.Result = "refused"
			}
			suffix := ""
			if line.SQLState != "" {
				suffix = " (SQLSTATE " + line.SQLState + ")"
			}
			say(errw, "migrate: "+sentence+suffix)
		}
		b, merr := json.Marshal(line)
		if merr != nil {
			say(errw, "migrate: the result could not be written")
			return exitCannot
		}
		if _, werr := fmt.Fprintln(out, string(b)); werr != nil && code == exitDone {
			say(errw, "migrate: the result could not be written")
			return exitCannot
		}
		return code
	}
	pw, err := readSecret(s.secretFile)
	if err != nil {
		return finish(err)
	}
	if err := checkDir(s.migrationsDir); err != nil {
		return finish(err)
	}
	cfg, err := connection(s, pw)
	if err != nil {
		return finish(err)
	}
	if corr, cerr := ids.New(clk, nil).ID("corr"); cerr == nil {
		line.CorrelationID = corr
	}
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return finish(stateError{kind: errUnreachable})
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return finish(stateError{kind: errUnreachable, state: sqlState(err)})
	}
	applied, err := db.Migrate(ctx, pool, os.DirFS(s.migrationsDir))
	if applied != nil {
		line.Applied = applied
	}
	line.AppliedCount = len(line.Applied)
	if err != nil {
		return finish(stateError{kind: errMigration, state: sqlState(err)})
	}
	return finish(nil)
}

// readSecret reads the secret file through the shared key-file reader (A518, A528): opened once without following a link or blocking on a special file; a regular file owned by the effective user,
// mode exactly 0400; at most maxSecretBytes. Every error of the reader is dropped on purpose: it must say nothing but that the file cannot be used. One final line break is not part of the secret;
// an empty secret, a blank one, and one with a line break or a NUL inside are refused.
func readSecret(path string) (string, error) {
	raw, err := keyfiles.ReadFile(path, maxSecretBytes)
	if err != nil {
		return "", errSecretFile
	}
	text := strings.TrimSuffix(string(raw), "\n")
	if strings.TrimSpace(text) == "" || strings.ContainsAny(text, "\n\x00") {
		return "", errSecretFile
	}
	return text, nil
}

// pgEnvironment is every variable the driver reads to complete a connection. They are removed from this process before the configuration is parsed, so nothing in the environment can name the host,
// the login, the mode or a certificate, supply a password, or set a runtime parameter: the flags and the secret file are the whole connection (A527). It reads no value.
var pgEnvironment = []string{
	"PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD", "PGPASSFILE", "PGAPPNAME", "PGCONNECT_TIMEOUT", "PGSSLMODE", "PGSSLKEY", "PGSSLCERT", "PGSSLSNI", "PGSSLROOTCERT",
	"PGSSLPASSWORD", "PGSSLNEGOTIATION", "PGTARGETSESSIONATTRS", "PGSERVICE", "PGSERVICEFILE", "PGTZ", "PGOPTIONS", "PGHOSTADDR", "PGSYSCONFDIR", "PGKRBSRVNAME", "PGGSSLIB",
}

// connection builds the driver's configuration in process. The non-secret parts are parsed from a keyword string that holds nothing but values already checked against their patterns; the secret is
// then set on the configuration directly. No string that holds the secret is ever made, so there is nothing to escape, to parse wrongly or to print. The parse error is dropped on purpose.
func connection(s settings, pw string) (*pgxpool.Config, error) {
	for _, name := range pgEnvironment {
		_ = os.Unsetenv(name) // an unset that fails changes nothing the parse could read
	}
	cfg, err := pgxpool.ParseConfig("host=" + s.host + " port=" + s.port + " dbname=" + s.database + " user=" + s.user + " sslmode=" + s.sslMode)
	if err != nil {
		return nil, errConfig
	}
	cc := cfg.ConnConfig
	if cc.Host != s.host || strconv.Itoa(int(cc.Port)) != s.port || cc.Database != s.database || cc.User != s.user {
		return nil, errConfig // the parts the driver holds are exactly the flags'
	}
	cc.Password = pw
	cc.ConnectTimeout = connectTimeout
	cc.RuntimeParams = map[string]string{"application_name": "noema-migrate"}
	cc.ValidateConnect = nil
	return cfg, nil
}

// checkDir refuses a path that is not a directory, and a directory with no .sql file: a mount that is wrong must not look like a database that is up to date.
func checkDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return errMigrationDir
	}
	names, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil || len(names) == 0 {
		return errMigrationDir
	}
	return nil
}
