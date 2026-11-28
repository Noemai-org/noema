// Command ops is the ops process of the tenant kill switch control plane (EXE-06; REQ-EXE-008, REQ-OPS-002, REQ-SEC-006; rulings A699, A710, A775). It is the only process that holds
// the tenants' admin emitter logins (A699): it builds one pool per tenant from the files of NOEMA_OPS_DSN_DIR, the admin signing ring from NOEMA_OPS_KEY_DIR, and serves the two routes
// of opslistener on NOEMA_OPS_LISTEN_ADDR over mutual TLS. Every setting is required and has no default (AGENT-16). An error names the setting and never a value or a path.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/cmd/ops/opslistener"
	"github.com/Noemai-org/noema/services/core/execution/controlplane"
	"github.com/Noemai-org/noema/services/core/execution/signing"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/grants/keyfiles"
)

// The settings of the ops process, beside the three of opslistener (EnvServerCertFile, EnvServerKeyFile, EnvClientCAFile). All are required; none has a default.
const (
	// EnvListenAddr is the address the listener binds, host:port (the bench: the ops compose service's own interface, never a public one).
	EnvListenAddr = "NOEMA_OPS_LISTEN_ADDR"
	// EnvDSNDir is the directory of the per-tenant admin emitter DSN files (A699): one regular file per tenant, named by the tenant ID, mode 0400.
	EnvDSNDir = "NOEMA_OPS_DSN_DIR"
	// EnvKeyDir and EnvKeyMaxActive are the directory of the admin signing keys (admin.ed25519.<n>.key) and how many may be mounted at once.
	EnvKeyDir       = "NOEMA_OPS_KEY_DIR"
	EnvKeyMaxActive = "NOEMA_OPS_KEY_MAX_ACTIVE"
	// EnvSwitchTimeoutMS bounds one kill switch change; the four below bound a client of the listener (headers, the whole request, an idle connection) and its header size.
	EnvSwitchTimeoutMS  = "NOEMA_OPS_SWITCH_TIMEOUT_MS"
	EnvHeaderTimeoutMS  = "NOEMA_OPS_HEADER_TIMEOUT_MS"
	EnvReadTimeoutMS    = "NOEMA_OPS_READ_TIMEOUT_MS"
	EnvIdleTimeoutMS    = "NOEMA_OPS_IDLE_TIMEOUT_MS"
	EnvMaxHeaderBytes   = "NOEMA_OPS_MAX_HEADER_BYTES"
	adminIdentity       = "admin"
	dsnFileMaxBytes     = 4096
	secretFileMaxBytes  = 16384
	maxMilliseconds     = 600000
	maxHeaderBytesLimit = 1 << 20
)

var (
	wholeRe      = regexp.MustCompile(`\A[1-9][0-9]{0,8}\z`)
	overlapRe    = regexp.MustCompile(`\A[1-9][0-9]{0,2}\z`)
	tenantFileRe = regexp.MustCompile(`\Aten_[0-9A-Za-z]{2,32}\z`)
)

// Env is what the process reads from outside, so a test gives it none of the real process.
type Env struct {
	Getenv  func(string) string
	ReadDir func(string) ([]os.DirEntry, error)
}

// settings is every setting once all are given and valid.
type settings struct {
	listen                            string
	dsnDir, keyDir                    string
	keyMaxActive                      int
	cert, key, ca                     string
	switchTimeout, header, read, idle time.Duration
	maxHeaderBytes                    int
}

func millis(env Env, name string) (time.Duration, error) {
	raw := env.Getenv(name)
	n, err := strconv.ParseInt(raw, 10, 64)
	if !wholeRe.MatchString(raw) || err != nil || n > maxMilliseconds {
		return 0, fmt.Errorf("ops: %s must be a whole positive number of milliseconds", name)
	}
	return time.Duration(n) * time.Millisecond, nil
}

// loadSettings reads every setting. A setting that is absent is named (all of them, in order); one that is present and invalid is an error naming it.
func loadSettings(env Env) (settings, error) {
	var s settings
	var missing []string
	for _, name := range []string{EnvListenAddr, EnvDSNDir, EnvKeyDir, EnvKeyMaxActive, opslistener.EnvServerCertFile, opslistener.EnvServerKeyFile, opslistener.EnvClientCAFile,
		EnvSwitchTimeoutMS, EnvHeaderTimeoutMS, EnvReadTimeoutMS, EnvIdleTimeoutMS, EnvMaxHeaderBytes} {
		if env.Getenv(name) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return settings{}, fmt.Errorf("ops: required settings are missing: %v", missing)
	}
	var err error
	if _, _, err = net.SplitHostPort(env.Getenv(EnvListenAddr)); err != nil {
		return settings{}, fmt.Errorf("ops: %s must be host:port", EnvListenAddr)
	}
	if !overlapRe.MatchString(env.Getenv(EnvKeyMaxActive)) {
		return settings{}, fmt.Errorf("ops: %s must be a whole number from 1 to 999", EnvKeyMaxActive)
	}
	s.keyMaxActive, _ = strconv.Atoi(env.Getenv(EnvKeyMaxActive))
	for _, t := range []struct {
		name string
		to   *time.Duration
	}{{EnvSwitchTimeoutMS, &s.switchTimeout}, {EnvHeaderTimeoutMS, &s.header}, {EnvReadTimeoutMS, &s.read}, {EnvIdleTimeoutMS, &s.idle}} {
		if *t.to, err = millis(env, t.name); err != nil {
			return settings{}, err
		}
	}
	n, err := strconv.Atoi(env.Getenv(EnvMaxHeaderBytes))
	if !wholeRe.MatchString(env.Getenv(EnvMaxHeaderBytes)) || err != nil || n > maxHeaderBytesLimit {
		return settings{}, fmt.Errorf("ops: %s must be a whole positive number of bytes no greater than %d", EnvMaxHeaderBytes, maxHeaderBytesLimit)
	}
	s.maxHeaderBytes = n
	s.listen, s.dsnDir, s.keyDir = env.Getenv(EnvListenAddr), env.Getenv(EnvDSNDir), env.Getenv(EnvKeyDir)
	s.cert, s.key, s.ca = env.Getenv(opslistener.EnvServerCertFile), env.Getenv(opslistener.EnvServerKeyFile), env.Getenv(opslistener.EnvClientCAFile)
	return s, nil
}

// pools is the per-tenant admin emitter logins, and nothing for any other tenant.
type pools map[string]*pgxpool.Pool

type beginner struct{ p *pgxpool.Pool }

func (b beginner) Begin(ctx context.Context) (pgx.Tx, error) { return b.p.Begin(ctx) }

// For gives the admin emitter login of one tenant; a tenant with no file has none.
func (p pools) For(tenantID string) (controlplane.Beginner, error) {
	pool, ok := p[tenantID]
	if !ok {
		return nil, errors.New("ops: no admin login for this tenant")
	}
	return beginner{pool}, nil
}

// loadPools makes one lazily connecting pool per file of the directory. The DSN is read by the checked secret reader and is never in an error.
func loadPools(ctx context.Context, env Env, dir string) (pools, func(), error) {
	entries, err := env.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return nil, nil, fmt.Errorf("ops: %s cannot be read or holds no tenant file", EnvDSNDir)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.Type().IsRegular() || !tenantFileRe.MatchString(e.Name()) {
			return nil, nil, fmt.Errorf("ops: %s: an entry is not a regular file named by a tenant ID", EnvDSNDir)
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	out := pools{}
	closeAll := func() {
		for _, p := range out {
			p.Close()
		}
	}
	for _, tenant := range names {
		raw, err := keyfiles.ReadFile(dir+"/"+tenant, dsnFileMaxBytes)
		if err != nil {
			closeAll()
			return nil, nil, fmt.Errorf("ops: %s: the file of tenant %s cannot be used as a secret", EnvDSNDir, tenant)
		}
		cfg, err := pgxpool.ParseConfig(string(raw))
		if err != nil { // dropped on purpose: it can quote the DSN
			closeAll()
			return nil, nil, fmt.Errorf("ops: %s: the DSN of tenant %s does not parse", EnvDSNDir, tenant)
		}
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			closeAll()
			return nil, nil, fmt.Errorf("ops: %s: the pool of tenant %s cannot be made", EnvDSNDir, tenant)
		}
		out[tenant] = pool
	}
	return out, closeAll, nil
}

// build makes the listener and the pools it uses; the caller serves it and closes the pools.
func build(ctx context.Context, env Env, clk clock.Clock, s settings) (*opslistener.Listener, func(), error) {
	ps, closePools, err := loadPools(ctx, env, s.dsnDir)
	if err != nil {
		return nil, nil, err
	}
	ring, err := signing.LoadRing(adminIdentity, s.keyDir, signing.Options{MaxActive: s.keyMaxActive})
	if err != nil {
		closePools()
		return nil, nil, fmt.Errorf("ops: %s: the admin signing keys cannot be loaded", EnvKeyDir)
	}
	gen := ids.New(clk, nil)
	sw := &controlplane.Switch{Logins: ps, Appender: &chain.Appender{Clock: clk, IDs: gen, Signers: map[string]chain.Signer{adminIdentity: ring}}, Timeout: s.switchTimeout}
	l, err := opslistener.New(opslistener.Config{
		ServerCertFile: s.cert, ServerKeyFile: s.key, ClientCAFile: s.ca,
		ReadFile: os.ReadFile, ReadSecret: func(p string) ([]byte, error) { return keyfiles.ReadFile(p, secretFileMaxBytes) },
		Clock: clk, Switch: sw, CorrelationIDs: func() (string, error) { return gen.ID("corr") },
		HeaderTimeout: s.header, ReadTimeout: s.read, IdleTimeout: s.idle, MaxHeaderBytes: s.maxHeaderBytes,
	})
	if err != nil {
		closePools()
		return nil, nil, err
	}
	return l, closePools, nil
}

func run(ctx context.Context, env Env, out io.Writer) error {
	s, err := loadSettings(env)
	if err != nil {
		return err
	}
	clk := clock.System{}
	l, closePools, err := build(ctx, env, clk, s)
	if err != nil {
		return err
	}
	defer closePools()
	ln, err := net.Listen("tcp", s.listen)
	if err != nil {
		return fmt.Errorf("ops: %s cannot be bound", EnvListenAddr)
	}
	fmt.Fprintf(out, "ops listening on %s\n", ln.Addr().String())
	return l.Serve(ctx, ln)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, Env{Getenv: os.Getenv, ReadDir: os.ReadDir}, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		stop()
		os.Exit(1)
	}
}
