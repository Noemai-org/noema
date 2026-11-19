package keyannounce_test

// A347 (fixed public text of an error that crosses a package boundary; AGENT-13): an error of keyannounce names neither the tenant, nor a key ID, nor the database's message
// (SQLSTATE text, server and login details), nor a file path or a DSN. The cause is kept behind Unwrap, so errors.Is and errors.As still reach it. Every case provokes the
// failure on the real stack (real PostgreSQL, real chain appender); the table covers every error return site of the package.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/recordsig"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/record/keyannounce"
)

// leakyText is the kind of text a dependency's error can carry: a tenant, a login, a server, a path.
const leakyText = "boom for ten_aa at host=db.internal password=hunter2 /etc/noema/keys/admin.pem postgres://admin@db.internal/noema"

var errLeaky = errors.New(leakyText)

// commonLeaks is what no error text of the package may hold. The lower-cased text is searched.
var commonLeaks = []string{"ten_aa", "sqlstate", "error:", "fatal", "pgx", "pq", "postgres://", "host=", "password", "/etc/", "/tmp", ".pem", ".json", "127.0.0.1", "localhost", "ed25519.", "record_emitter", "record_read", "terminating", "permission denied", "closed pool", "hunter2", "db.internal"}

func assertFixedText(t *testing.T, w *world, err error, extra ...string) {
	t.Helper()
	text := strings.ToLower(err.Error())
	for _, leak := range append(append([]string{strings.ToLower(w.dsn)}, commonLeaks...), extra...) {
		if leak != "" && strings.Contains(text, strings.ToLower(leak)) {
			t.Errorf("the error text carries %q: %q", leak, err.Error())
		}
	}
	if !strings.HasPrefix(err.Error(), "keyannounce: ") {
		t.Errorf("the error text %q does not start with the package's prefix", err.Error())
	}
	if errors.Unwrap(err) == nil {
		t.Errorf("the cause is not kept behind Unwrap: %q", err.Error())
	}
}

// holdTenantLock holds the per-tenant announcement lock of the tenant on a connection of its own, so a run that needs the lock waits for it. It returns the release.
func holdTenantLock(t *testing.T, w *world, tenant string) func() {
	t.Helper()
	cfg, err := pgx.ParseConfig(w.dsn)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.ConnectConfig(ctx(t), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx(t), `SELECT pg_advisory_lock(hashtextextended($1, 0))`, "noema:key_rotated:"+tenant); err != nil {
		t.Fatal(err)
	}
	var once bool
	release := func() {
		if !once {
			once = true
			_ = conn.Close(context.Background())
		}
	}
	t.Cleanup(release)
	return release
}

func waitingLocks(t *testing.T, w *world) int {
	t.Helper()
	var n int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// killOnCommit hands out the real transaction, but ends its server session just before the commit: a connection lost at the last step.
type killOnCommit struct{ keyannounce.TxBeginner }

func (k killOnCommit) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	tx, err := k.TxBeginner.BeginTx(ctx, o)
	if err != nil {
		return nil, err
	}
	return &killingTx{Tx: tx}, nil
}

type killingTx struct{ pgx.Tx }

func (k *killingTx) Commit(ctx context.Context) error {
	_, _ = k.Tx.Exec(ctx, `SELECT pg_terminate_backend(pg_backend_pid())`)
	return k.Tx.Commit(ctx)
}

type shapelessRegistry struct{}

func (shapelessRegistry) KeyIDs(context.Context) ([]string, error) {
	return []string{"Admin.RSA.0"}, nil
}

type leakyRegistry struct{}

func (leakyRegistry) KeyIDs(context.Context) ([]string, error) { return nil, errLeaky }

type brokenEntropy struct{}

func (brokenEntropy) Read([]byte) (int, error) { return 0, errLeaky }

type leakySigner struct{}

func (leakySigner) SignRecordEvent(map[string]any) (string, string, []byte, error) {
	return "", "", nil, errLeaky
}

// A347: the table. Each case is one error return site of Announce (and of the key registry it reads), provoked on the real stack.
func TestEveryErrorOfAnnounceHasAFixedPublicTextAndKeepsItsCause(t *testing.T) {
	cases := []struct {
		name  string
		extra []string
		run   func(t *testing.T, w *world, ten keyannounce.Tenant) error
		cause func(t *testing.T, err error)
	}{
		{name: "begin: the admin login's pool is closed",
			run: func(t *testing.T, w *world, ten keyannounce.Tenant) error {
				ten.Admin.(*pgxpool.Pool).Close()
				_, err := w.ann.Announce(ctx(t), ten)
				return err
			},
			cause: func(t *testing.T, err error) {
				if !strings.Contains(errors.Unwrap(err).Error(), "closed pool") {
					t.Errorf("the cause is %q, want the pool's own error", errors.Unwrap(err))
				}
			}},
		{name: "lock: the session is terminated while the statement waits",
			run: func(t *testing.T, w *world, ten keyannounce.Tenant) error {
				holdTenantLock(t, w, tenantA)
				c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				done := make(chan error, 1)
				go func() { _, err := w.ann.Announce(c, ten); done <- err }()
				deadline := time.Now().Add(15 * time.Second)
				for waitingLocks(t, w) == 0 {
					if time.Now().After(deadline) {
						t.Fatal("the announcement never queued on its lock")
					}
					time.Sleep(20 * time.Millisecond)
				}
				if _, err := w.admin.Exec(ctx(t), `SELECT pg_terminate_backend(pid) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-done:
					return err
				case <-time.After(20 * time.Second):
					t.Fatal("Announce did not return after its waiting session was terminated")
					return nil
				}
			},
			cause: func(t *testing.T, err error) {
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) {
					t.Errorf("the driver's PgError is not reachable: %v", err)
				}
			}},
		{name: "lock: the caller's deadline ends the wait",
			run: func(t *testing.T, w *world, ten keyannounce.Tenant) error {
				holdTenantLock(t, w, tenantA)
				c, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
				defer cancel()
				_, err := w.ann.Announce(c, ten)
				return err
			},
			cause: func(t *testing.T, err error) {
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("the deadline is not reachable: %v", err)
				}
			}},
		{name: "registry: the login has no SELECT on signing_key",
			run: func(t *testing.T, w *world, ten keyannounce.Tenant) error {
				a := w.announcer(map[string]chain.Signer{"admin": w.signers["admin"]})
				a.Keys = keyannounce.PoolKeys{Q: ten.Admin.(*pgxpool.Pool)}
				_, err := a.Announce(ctx(t), ten)
				return err
			},
			cause: func(t *testing.T, err error) {
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
					t.Errorf("the driver's PgError 42501 is not reachable: %v", err)
				}
			}},
		{name: "registry: another implementation's error, with a leaky text",
			extra: []string{"boom"},
			run: func(t *testing.T, w *world, ten keyannounce.Tenant) error {
				a := w.announcer(map[string]chain.Signer{"admin": w.signers["admin"]})
				a.Keys = leakyRegistry{}
				_, err := a.Announce(ctx(t), ten)
				return err
			},
			cause: func(t *testing.T, err error) {
				if !errors.Is(err, errLeaky) {
					t.Errorf("the cause is not reachable: %v", err)
				}
			}},
		{name: "registry: a key ID is not of the K1 shape",
			extra: []string{"Admin.RSA"},
			run: func(t *testing.T, w *world, ten keyannounce.Tenant) error {
				a := w.announcer(map[string]chain.Signer{"admin": w.signers["admin"]})
				a.Keys = shapelessRegistry{}
				_, err := a.Announce(ctx(t), ten)
				return err
			},
			cause: func(t *testing.T, err error) {}},
		{name: "chain: the reader is not a record_read login",
			run: func(t *testing.T, w *world, ten keyannounce.Tenant) error {
				ten.Reader = testpg.TenantLogin(t, w.admin, w.dsn, "execution_rw", tenantA)
				_, err := w.ann.Announce(ctx(t), ten)
				return err
			},
			cause: func(t *testing.T, err error) {
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) {
					t.Errorf("the driver's PgError is not reachable: %v", err)
				}
			}},
		{name: "correlation ID: the entropy fails",
			extra: []string{"boom"},
			run: func(t *testing.T, w *world, ten keyannounce.Tenant) error {
				a := w.announcer(map[string]chain.Signer{"admin": w.signers["admin"]})
				a.Appender.IDs = ids.New(w.clk, brokenEntropy{})
				_, err := a.Announce(ctx(t), ten)
				return err
			},
			cause: func(t *testing.T, err error) {
				if !errors.Is(err, errLeaky) {
					t.Errorf("the cause is not reachable: %v", err)
				}
			}},
		{name: "append: the signer fails with a leaky text",
			extra: []string{"boom"},
			run: func(t *testing.T, w *world, ten keyannounce.Tenant) error {
				_, err := w.announcer(map[string]chain.Signer{"admin": leakySigner{}}).Announce(ctx(t), ten)
				return err
			},
			cause: func(t *testing.T, err error) {
				if !errors.Is(err, errLeaky) {
					t.Errorf("the cause is not reachable: %v", err)
				}
			}},
		{name: "append: there is no signer for admin",
			run: func(t *testing.T, w *world, ten keyannounce.Tenant) error {
				_, err := w.announcer(map[string]chain.Signer{}).Announce(ctx(t), ten)
				return err
			},
			cause: func(t *testing.T, err error) {}},
		{name: "append: the database refuses the signature",
			run: func(t *testing.T, w *world, ten keyannounce.Tenant) error {
				_, err := w.announcer(recordsig.Local("another world", nil, "admin")).Announce(ctx(t), ten)
				return err
			},
			cause: func(t *testing.T, err error) {
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) {
					t.Errorf("the driver's PgError is not reachable: %v", err)
				}
			}},
		{name: "append: the login is not the admin emitter's",
			run: func(t *testing.T, w *world, ten keyannounce.Tenant) error {
				ten.Admin = testpg.TenantLogin(t, w.admin, w.dsn, "trust_rw", tenantA)
				_, err := w.ann.Announce(ctx(t), ten)
				return err
			},
			cause: func(t *testing.T, err error) {
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) {
					t.Errorf("the driver's PgError is not reachable: %v", err)
				}
			}},
		{name: "commit: the session is terminated before the commit",
			run: func(t *testing.T, w *world, ten keyannounce.Tenant) error {
				ten.Admin = killOnCommit{ten.Admin}
				_, err := w.ann.Announce(ctx(t), ten)
				return err
			},
			cause: func(t *testing.T, err error) {}},
		{name: "tenant: the ID does not match the contract",
			extra: []string{"not a tenant"},
			run: func(t *testing.T, w *world, ten keyannounce.Tenant) error {
				ten.ID = "not a tenant ten_aa"
				_, err := w.ann.Announce(ctx(t), ten)
				return err
			},
			cause: func(t *testing.T, err error) {}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			ten := w.tenant(t, tenantA)
			err := c.run(t, w, ten)
			if err == nil {
				t.Fatal("no error, want one")
			}
			text := strings.ToLower(err.Error())
			for _, leak := range append(append([]string{strings.ToLower(w.dsn)}, commonLeaks...), c.extra...) {
				if strings.Contains(text, strings.ToLower(leak)) && !(c.name == "tenant: the ID does not match the contract" && leak == "ten_aa") {
					t.Errorf("the error text carries %q: %q", leak, err.Error())
				}
			}
			if !strings.HasPrefix(err.Error(), "keyannounce: ") {
				t.Errorf("the error text %q does not start with the package's prefix", err.Error())
			}
			c.cause(t, err)
			if c.name != "tenant: the ID does not match the contract" && c.name != "registry: a key ID is not of the K1 shape" && errors.Unwrap(err) == nil {
				t.Errorf("the cause is not kept behind Unwrap: %q", err.Error())
			}
		})
	}
}

// PoolKeys.KeyIDs is exported: its errors too (the read, the scan, the rows).
func TestPoolKeysErrorsHaveAFixedPublicTextAndKeepTheirCause(t *testing.T) {
	w := newWorld(t)
	t.Run("the read fails: the pool is closed", func(t *testing.T) {
		p := testpg.TenantLogin(t, w.admin, w.dsn, "record_read", tenantA)
		p.Close()
		_, err := keyannounce.PoolKeys{Q: p}.KeyIDs(ctx(t))
		if err == nil {
			t.Fatal("no error")
		}
		assertFixedText(t, w, err)
	})
	t.Run("the read is refused by the database", func(t *testing.T) {
		p := testpg.TenantLogin(t, w.admin, w.dsn, "execution_rw", tenantA)
		_, err := keyannounce.PoolKeys{Q: p}.KeyIDs(ctx(t))
		var pgErr *pgconn.PgError
		if err == nil || !errors.As(err, &pgErr) {
			t.Fatalf("err = %v, want one that holds the driver's PgError", err)
		}
		assertFixedText(t, w, err)
	})
	t.Run("the rows end with an error", func(t *testing.T) {
		_, err := keyannounce.PoolKeys{Q: failingRows{}}.KeyIDs(ctx(t))
		if err == nil || !errors.Is(err, errLeaky) {
			t.Fatalf("err = %v, want one that holds the rows' error", err)
		}
		assertFixedText(t, w, err, "boom")
	})
	t.Run("a row cannot be scanned", func(t *testing.T) {
		_, err := keyannounce.PoolKeys{Q: scanFailingRows{}}.KeyIDs(ctx(t))
		if err == nil || !errors.Is(err, errLeaky) {
			t.Fatalf("err = %v, want one that holds the scan's error", err)
		}
		assertFixedText(t, w, err, "boom")
	})
	t.Run("the context has no deadline", func(t *testing.T) {
		_, err := keyannounce.PoolKeys{Q: w.verifier}.KeyIDs(context.Background())
		if err == nil {
			t.Fatal("no error")
		}
		if !strings.HasPrefix(err.Error(), "keyannounce: ") {
			t.Errorf("the text %q does not start with the package's prefix", err.Error())
		}
	})
}

// failingRows and scanFailingRows are a chain.Queryer whose result misbehaves in the two places a real connection cannot be made to for a one-column result.
type failingRows struct{}

func (failingRows) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return &stubRows{errAtEnd: errLeaky}, nil
}

type scanFailingRows struct{}

func (scanFailingRows) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return &stubRows{rows: 1, scanErr: errLeaky}, nil
}

type stubRows struct {
	rows     int
	scanErr  error
	errAtEnd error
}

func (s *stubRows) Close()                                       {}
func (s *stubRows) Err() error                                   { return s.errAtEnd }
func (s *stubRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (s *stubRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (s *stubRows) Next() bool {
	if s.rows > 0 {
		s.rows--
		return true
	}
	return false
}
func (s *stubRows) Scan(...any) error      { return s.scanErr }
func (s *stubRows) Values() ([]any, error) { return nil, nil }
func (s *stubRows) RawValues() [][]byte    { return nil }
func (s *stubRows) Conn() *pgx.Conn        { return nil }
