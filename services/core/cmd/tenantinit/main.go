// Command tenantinit registers the core-trust public keys, once, before the stack serves (TRU-02; rulings A484 and A489; INV-12; REQ-SEC-004, REQ-SEC-005). It creates no tenant row: tenant rows
// have one creator, create-tenants.sh (A353, A489).
// See README.md.
//
//	tenantinit --dsn-file FILE --key-dir DIR --key-max-active N
//
// The login in the DSN file is one that may call key_register, which is executable by its owner and the superuser only (migration 0013); the database refuses any other login. The key directory holds the core-trust signing keys, read with execution/signing.LoadRing; only the public
// half of each is derived and registered, and no private byte is returned, printed or logged. Nothing secret is an argument: the DSN and the keys come from files.
//
// Output: on success one JSON line on stdout with two counts and nothing else (never a name, a key or a path). When the exit code is not 0, nothing on stdout and one fixed sentence on stderr.
// Exit codes: 0 done, or already done; 1 refused (a version registered with another public key or no longer active, a key the database refuses, a login the
// database refuses); 2 usage; 3 the command could not run (a DSN file, a key directory or a database that cannot be used).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Noemai-org/noema/services/core/execution/signing"
)

const (
	exitDone    = 0
	exitRefused = 1
	exitUsage   = 2
	exitFailure = 3

	// runTimeout bounds the whole run: every call takes a context with this deadline. An operating limit of this command, not a safety threshold.
	runTimeout   = 2 * time.Minute
	closeTimeout = 5 * time.Second

	maxDSNBytes = 8 << 10
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// dialFunc opens the one connection of a run from the DSN file. Tests pass a counting wrapper of connect.
type dialFunc func(ctx context.Context, dsnFile string) (*pgx.Conn, error)

func run(args []string, out, errw io.Writer) int { return runWith(args, out, errw, connect) }

var (
	errKeysUnusable = errors.New("tenantinit: the signing keys cannot be loaded")
	errDSNUnusable  = errors.New("tenantinit: the DSN file cannot be used")
)

// failure maps an error to the exit code and the fixed sentence. The sentence never carries the error's text (A347); the SQLSTATE of a database error is a fixed five-character code.
func failure(err error) (code int, sentence string) {
	switch {
	case errors.Is(err, errKeyDiffers):
		return exitRefused, "a different public key is already registered under this key version; nothing was changed"
	case errors.Is(err, errKeyNotActive):
		return exitRefused, "a key of this version is registered but is no longer active; nothing was changed"
	case errors.Is(err, errKeyRefused):
		return exitRefused, "the database refused to register a key (a version that is not newer than the registered ones, or too many active keys); nothing was changed"
	case errors.Is(err, errLoginDenied):
		return exitRefused, "the login may not register keys (only the owner of key_register and the operator's superuser may); nothing was changed"
	case errors.Is(err, errKeysUnusable):
		return exitFailure, "the signing keys cannot be loaded; nothing was changed"
	case errors.Is(err, errDSNUnusable):
		return exitFailure, "the DSN file cannot be used"
	}
	var ce connectError
	if errors.As(err, &ce) {
		return exitFailure, "the database cannot be reached" + suffix(ce.state)
	}
	return exitFailure, "the initialisation could not be completed; nothing was changed" + suffix(sqlState(err))
}

func suffix(state string) string {
	if state == "" {
		return ""
	}
	return " (SQLSTATE " + state + ")"
}

// sqlState is the five-character SQLSTATE of a database error, the one part of it that is a fixed code.
func sqlState(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && len(pg.Code) == 5 {
		return pg.Code
	}
	return ""
}

// say writes one diagnostic line to errw. The exit code and the result line carry the outcome, so a failed write to errw is reported nowhere else (it is the last channel).
func say(errw io.Writer, line string) { _, _ = fmt.Fprintln(errw, line) }

func runWith(args []string, out, errw io.Writer, dial dialFunc) int {
	s, ok := parseArgs(args)
	if !ok {
		say(errw, "tenantinit: "+usage)
		return exitUsage
	}
	fail := func(err error) int {
		code, sentence := failure(err)
		say(errw, "tenantinit: "+sentence)
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	// The keys are read before the database is opened: a key file the loader refuses stops the run with nothing touched.
	keys, err := loadPublicKeys(s.keyDir, s.keyMaxActive)
	if err != nil {
		return fail(err)
	}
	conn, err := dial(ctx, s.dsnFile)
	if err != nil {
		return fail(err)
	}
	defer func() {
		cctx, ccancel := context.WithTimeout(context.Background(), closeTimeout)
		defer ccancel()
		_ = conn.Close(cctx)
	}()
	c, err := apply(ctx, conn, keys)
	if err != nil {
		return fail(err)
	}
	b, err := json.Marshal(c)
	if err != nil {
		say(errw, "tenantinit: the result could not be written")
		return exitFailure
	}
	if _, err := fmt.Fprintln(out, string(b)); err != nil {
		say(errw, "tenantinit: the result could not be written")
		return exitFailure
	}
	return exitDone
}

// loadPublicKeys reads the core-trust keys with the signing package's own loader (mode, name, owner, link and size checks) and returns their public halves in ascending version. The loader's
// errors never repeat a key's contents; none is printed either way.
func loadPublicKeys(dir string, maxActive int) ([]registeredKey, error) {
	ring, err := signing.LoadRing(keyIdentity, dir, signing.Options{MaxActive: maxActive})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errKeysUnusable, err)
	}
	var keys []registeredKey
	for id, pub := range ring.PublicKeys() {
		v, err := strconv.Atoi(id[strings.LastIndex(id, ".")+1:])
		if err != nil {
			return nil, errKeysUnusable
		}
		keys = append(keys, registeredKey{version: v, public: pub})
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].version < keys[j].version })
	return keys, nil
}

// connect reads the DSN from its file (a regular file, not a link, owned by the caller, mode without group or other bits, not empty) and opens one connection. No error carries the DSN or any part of it.
func connect(ctx context.Context, path string) (*pgx.Conn, error) {
	return connectAs(ctx, path, uint32(os.Geteuid()))
}

// connectAs is connect for a caller whose effective uid is euid. The DSN file is a regular file (not a link), owned by the caller, with no group or other permission bit, as the
// other one-shots' secret files are (SR on noema#852).
func connectAs(ctx context.Context, path string, euid uint32) (*pgx.Conn, error) {
	raw, err := readDSNFile(path, euid, nil)
	dsn := strings.TrimSpace(string(raw))
	if err != nil || dsn == "" {
		return nil, errDSNUnusable
	}
	cfg, err := pgx.ParseConfig(dsn) // the parse error is dropped on purpose: it can quote the DSN
	if err != nil {
		return nil, errDSNUnusable
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, connectError{state: sqlState(err)}
	}
	return conn, nil
}

// readDSNFile returns the bytes of the DSN file. The path is opened once, without following a link and without waiting on a FIFO, and everything is then checked on that descriptor and
// read from it: a regular file, at most maxDSNBytes, with no group or other permission bit, owned by euid. Nothing is looked at by path a second time, so replacing the path after the
// open changes nothing about the file that was checked (Codex on noema#852). afterCheck, when not nil, runs between the checks and the read: a test uses it to replace the path at that moment.
func readDSNFile(path string, euid uint32, afterCheck func()) ([]byte, error) {
	if path == "" {
		return nil, errDSNUnusable
	}
	f, err := openDSN(path)
	if err != nil {
		return nil, errDSNUnusable
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxDSNBytes || info.Mode().Perm()&0o077 != 0 {
		return nil, errDSNUnusable
	}
	if owner, ok := fileOwner(info); !ok || owner != euid {
		return nil, errDSNUnusable
	}
	if afterCheck != nil {
		afterCheck()
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxDSNBytes+1))
	if err != nil || len(raw) > maxDSNBytes {
		return nil, errDSNUnusable
	}
	return raw, nil
}

// connectError is a connection that could not be opened. It keeps the SQLSTATE of the database's refusal, when there is one, and nothing else: a driver's text can quote the DSN.
type connectError struct{ state string }

func (connectError) Error() string { return "tenantinit: the database cannot be reached" }
