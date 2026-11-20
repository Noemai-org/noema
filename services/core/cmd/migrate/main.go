// Command migrate applies the database migrations once and exits (EXE-08; A484; REQ-DATA-001; INV-30). See README.md.
//
//	migrate --dsn-file FILE --migrations-dir DIR
//
// The DSN is the migration login's and is read from a file; it is never an argument, never read from the environment and never printed. The migrations are the .sql files of the directory,
// applied by platform/db.Migrate in file-name order, each in its own transaction, forward-only.
//
// Output: one JSON line on stdout (PERD 44.2 fields, never a DSN, a login, a SQL text or a driver's text) and, when the exit code is not 0, one fixed sentence on stderr.
// Exit codes: 0 migrated, or already up to date; 1 a migration was refused or failed (a changed or missing applied file, a statement the database refused); 2 the command could not run
// (a bad argument, a DSN file or a directory that cannot be used, a database that cannot be reached).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
)

const (
	exitDone    = 0
	exitRefused = 1
	exitCannot  = 2

	// runTimeout bounds the whole run: db.Migrate refuses a context without a deadline. An operating limit of this command, not a safety threshold.
	runTimeout = 5 * time.Minute

	maxDSNBytes = 8 << 10

	usage = "usage: migrate --dsn-file FILE --migrations-dir DIR"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, clock.System{})) }

type settings struct{ dsnFile, migrationsDir string }

func parseArgs(args []string) (settings, bool) {
	var s settings
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&s.dsnFile, "dsn-file", "", "a file holding the DSN of the migration login (no default)")
	fs.StringVar(&s.migrationsDir, "migrations-dir", "", "the directory of the .sql migrations (no default)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return s, false
	}
	return s, s.dsnFile != "" && s.migrationsDir != ""
}

// Typed reasons a run ends. The sentence for each is fixed; no error's text is ever printed.
var (
	errDSNFile      = errors.New("migrate: the DSN file cannot be used")
	errDSNParse     = errors.New("migrate: the DSN does not parse")
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
	case errors.Is(err, errDSNFile):
		return exitCannot, "dsn_file_unusable", "the DSN file cannot be used"
	case errors.Is(err, errDSNParse):
		return exitCannot, "dsn_unusable", "the DSN does not parse"
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

func run(args []string, out, errw io.Writer, clk clock.Clock) int {
	s, ok := parseArgs(args)
	if !ok {
		fmt.Fprintln(errw, "migrate: "+usage)
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
			fmt.Fprintln(errw, "migrate: "+sentence+suffix)
		}
		b, merr := json.Marshal(line)
		if merr != nil {
			fmt.Fprintln(errw, "migrate: the result could not be written")
			return exitCannot
		}
		if _, werr := fmt.Fprintln(out, string(b)); werr != nil && code == exitDone {
			fmt.Fprintln(errw, "migrate: the result could not be written")
			return exitCannot
		}
		return code
	}
	dsn, err := readDSN(s.dsnFile)
	if err != nil {
		return finish(err)
	}
	if err := checkDir(s.migrationsDir); err != nil {
		return finish(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn) // the parse error is dropped on purpose: it can quote the DSN
	if err != nil {
		return finish(errDSNParse)
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

// readDSN reads the DSN from its file: a regular file (not a link, not a directory), not empty, not larger than maxDSNBytes. No error carries the DSN or any part of it.
func readDSN(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxDSNBytes {
		return "", errDSNFile
	}
	raw, err := os.ReadFile(path)
	dsn := strings.TrimSpace(string(raw))
	if err != nil || dsn == "" {
		return "", errDSNFile
	}
	return dsn, nil
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
