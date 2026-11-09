// Package testpg starts a throwaway PostgreSQL server for tests that need a real database (REQ-TST-004:
// Testcontainers or a real server, never an in-memory fake). Tests only; no production code imports it.
//
// If NOEMA_TEST_DATABASE_URL is set it is used as it is (CI may point it at a service container). Otherwise a
// server is started from the PostgreSQL binaries on the machine, on a private directory and a free port, and
// removed when the test binary exits. When neither exists the test fails: it never silently skips.
package testpg

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const envURL = "NOEMA_TEST_DATABASE_URL"

var (
	once    sync.Once
	baseURL string
	baseErr error
	stop    func()

	dbCounter atomic.Int64
)

// Main starts the shared server for a test package and runs the tests. Call it from TestMain:
//
//	func TestMain(m *testing.M) { testpg.Main(m) }
func Main(m *testing.M) {
	code := m.Run()
	if stop != nil {
		stop()
	}
	os.Exit(code)
}

// ServerURL returns the URL of the shared server's maintenance database, connected as the superuser, for tests that
// need to create roles or databases of their own (a second service with its own database).
func ServerURL(t testing.TB) string {
	t.Helper()
	once.Do(func() {
		if baseURL, baseErr = start(); baseErr == nil {
			baseErr = ensureRoles(baseURL)
		}
	})
	if baseErr != nil {
		t.Fatalf("testpg: %v", baseErr)
	}
	return baseURL
}

// NewDatabase creates an empty database on the shared server and returns its pool, connected as the
// superuser (the migrating role). The database is dropped when the test ends.
func NewDatabase(t testing.TB) (*pgxpool.Pool, string) {
	t.Helper()
	once.Do(func() {
		if baseURL, baseErr = start(); baseErr == nil {
			baseErr = ensureRoles(baseURL)
		}
	})
	if baseErr != nil {
		t.Fatalf("testpg: %v", baseErr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatalf("testpg: connect: %v", err)
	}
	defer admin.Close()
	name := fmt.Sprintf("t_%d_%d", os.Getpid(), dbCounter.Add(1))
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("testpg: create database: %v", err)
	}
	dsn := strings.Replace(baseURL, "/postgres?", "/"+name+"?", 1)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("testpg: connect to %s: %v", name, err)
	}
	// As deploy/compose/postgres/init.sh does: record_append holds CREATE on public while the migrations run, because
	// migration 0001 hands tables and functions to it (PostgreSQL needs the new owner to hold CREATE). Migration 0010
	// (EXE-08, DEBT-05) revokes it; the tests pin that.
	if _, err := pool.Exec(ctx, "GRANT CREATE ON SCHEMA public TO record_append"); err != nil {
		t.Fatalf("testpg: grant CREATE to record_append: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		a, err := pgxpool.New(c, baseURL)
		if err != nil {
			return
		}
		defer a.Close()
		_, _ = a.Exec(c, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	return pool, dsn
}

// clusterRoles are the NOLOGIN group roles that deploy/compose/postgres/init.sh creates. A migration never creates a role (the
// migrating role cannot), so the test cluster creates them once, as init.sh does, before any database is migrated.
var clusterRoles = []string{"trust_rw", "execution_rw", "outcome_rw", "evidence_rw", "orchestrator_rw", "record_append", "record_read", "record_verify", "record_emitter", "noema_migrator"}

func ensureRoles(url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		return fmt.Errorf("connect to create the cluster roles: %w", err)
	}
	defer admin.Close()
	// One advisory lock: several test binaries may share one server and start together.
	conn, err := admin.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(7770001)`); err != nil {
		return err
	}
	defer func() { _, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(7770001)`) }()
	for _, r := range clusterRoles {
		if _, err := conn.Exec(ctx, `DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '`+r+`') THEN CREATE ROLE `+r+` NOLOGIN; END IF; END $$`); err != nil {
			return fmt.Errorf("create role %s: %w", r, err)
		}
	}
	return nil
}

// powerOf is the tenant_login power of each group role that logs in per tenant.
var powerOf = map[string]string{
	"trust_rw": "trust", "execution_rw": "execution", "outcome_rw": "outcome", "evidence_rw": "evidence",
	"orchestrator_rw": "orchestrator", "record_read": "record_read", "record_emitter": "record_emitter",
}

// VerifierLogin creates the all-tenant verification login of REC-05 (noema_verify in Compose): a LOGIN role that is a member of
// record_verify and of nothing else, with no tenant mapping, and returns a pool whose connections authenticate as it. The role is
// dropped when the test ends.
func VerifierLogin(t testing.TB, admin *pgxpool.Pool, dsn string) *pgxpool.Pool {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("testpg: parse dsn: %v", err)
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	role := "record_verify__" + dbName
	quoted := pgx.Identifier{role}.Sanitize()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := admin.Exec(ctx, `CREATE ROLE `+quoted+` LOGIN IN ROLE record_verify`); err != nil {
		t.Fatalf("testpg: create %s: %v", role, err)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if a, err := pgxpool.New(c, strings.Replace(dsn, "/"+dbName+"?", "/postgres?", 1)); err == nil {
			_, _ = a.Exec(c, `DROP ROLE IF EXISTS `+quoted)
			a.Close()
		}
	})
	u.User = url.User(role)
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatalf("testpg: connect as %s: %v", role, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TenantLogin creates a LOGIN role for one tenant and one power, maps it to the tenant in tenant_login, and returns a pool
// whose connections authenticate as that login (DEBT-04 option A). SET ROLE would leave session_user as the administrator,
// so the Record functions would not see a tenant. The tenant row must exist. The role is dropped when the test ends.
// group is the per-power group role, for example "execution_rw".
func TenantLogin(t testing.TB, admin *pgxpool.Pool, dsn, group, tenant string) *pgxpool.Pool {
	t.Helper()
	return login(t, admin, dsn, group, tenant, "")
}

// EmitterLogin is TenantLogin for a login that the mapping binds to one emitter that is not a power (DEBT-07, docs#106 R16), for
// example core-edge. It is a member of record_emitter, which holds the Record append functions and nothing else, never of a
// power role: record_insert_t lets it write that emitter and the database lets it do nothing more.
func EmitterLogin(t testing.TB, admin *pgxpool.Pool, dsn, tenant, emitter string) *pgxpool.Pool {
	t.Helper()
	return login(t, admin, dsn, "record_emitter", tenant, emitter)
}

func login(t testing.TB, admin *pgxpool.Pool, dsn, group, tenant, emitter string) *pgxpool.Pool {
	t.Helper()
	power, ok := powerOf[group]
	if !ok {
		t.Fatalf("testpg: %q is not a group role that logs in per tenant", group)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("testpg: parse dsn: %v", err)
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	role := group + "__" + tenant + "_" + dbName
	if emitter != "" {
		sum := sha256.Sum256([]byte(emitter))
		role = fmt.Sprintf("%s__%s_e%x_%s", group, tenant, sum[:3], dbName)
	}
	quoted := pgx.Identifier{role}.Sanitize()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := admin.Exec(ctx, `CREATE ROLE `+quoted+` LOGIN IN ROLE `+pgx.Identifier{group}.Sanitize()); err != nil {
		t.Fatalf("testpg: create %s: %v", role, err)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if a, err := pgxpool.New(c, strings.Replace(dsn, "/"+dbName+"?", "/postgres?", 1)); err == nil {
			_, _ = a.Exec(c, `DROP ROLE IF EXISTS `+quoted)
			a.Close()
		}
	})
	if _, err := admin.Exec(ctx, `INSERT INTO tenant_login (login_role, power, tenant_id, emitter) VALUES ($1, $2, $3, NULLIF($4, ''))`, role, power, tenant, emitter); err != nil {
		t.Fatalf("testpg: map %s to %s: %v", role, tenant, err)
	}
	u.User = url.User(role)
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatalf("testpg: connect as %s: %v", role, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func start() (string, error) {
	if u := os.Getenv(envURL); u != "" {
		return u, nil
	}
	bin, err := findBin()
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "noema-pg-")
	if err != nil {
		return "", err
	}
	asPG := []string{}
	if os.Geteuid() == 0 {
		if err := exec.Command("chown", "-R", "postgres", dir).Run(); err != nil {
			return "", fmt.Errorf("chown %s: %w", dir, err)
		}
		asPG = []string{"runuser", "-u", "postgres", "--"}
	}
	run := func(args ...string) ([]byte, error) {
		full := append(append([]string{}, asPG...), args...)
		return exec.Command(full[0], full[1:]...).CombinedOutput()
	}
	data := filepath.Join(dir, "data")
	if out, err := run(filepath.Join(bin, "initdb"), "-D", data, "-A", "trust", "-U", "postgres"); err != nil {
		return "", fmt.Errorf("initdb: %v: %s", err, out)
	}
	port, err := freePort()
	if err != nil {
		return "", err
	}
	opts := fmt.Sprintf("-p %d -k %s -c listen_addresses=127.0.0.1 -c fsync=off -c max_connections=200", port, dir)
	if out, err := run(filepath.Join(bin, "pg_ctl"), "-D", data, "-o", opts, "-l", filepath.Join(dir, "log"), "-w", "start"); err != nil {
		return "", fmt.Errorf("pg_ctl start: %v: %s", err, out)
	}
	stop = func() {
		_, _ = run(filepath.Join(bin, "pg_ctl"), "-D", data, "-m", "immediate", "stop")
		_ = os.RemoveAll(dir)
	}
	return fmt.Sprintf("postgres://postgres@127.0.0.1:%d/postgres?sslmode=disable", port), nil
}

func findBin() (string, error) {
	matches, _ := filepath.Glob("/usr/lib/postgresql/*/bin")
	if len(matches) > 0 {
		sort.Strings(matches)
		return matches[len(matches)-1], nil
	}
	if p, err := exec.LookPath("initdb"); err == nil {
		return filepath.Dir(p), nil
	}
	return "", fmt.Errorf("no PostgreSQL server binaries found; set %s to a server URL instead", envURL)
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }() // the port number is all that is needed; the listener only reserved it
	return l.Addr().(*net.TCPAddr).Port, nil
}

// Synthetic values for a Decision Record that carries its bound fields (ruling of 8 Nov 2026 on noema#329: a record without them is invalid, and a grant
// must agree with its record). They are SYNTHETIC; tests that insert a record or a grant directly append BoundFieldsSQL to the document.
const (
	FixtureIntentHash    = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	FixturePolicyVersion = "pol_1@1"
	FixtureIssuedAt      = "2026-11-07T09:00:00.000Z"
	FixtureRecordExpires = "2099-01-01T00:00:00.000Z"
	// BoundFieldsSQL is a jsonb expression holding the fields of a Decision Record that the database requires (intent_id, correlation_id, policy_version,
	// intent_hash, issued_at, expires_at, and the three authority layers at PASS, which an ALLOW needs). Append it to a document with ||.
	BoundFieldsSQL = `jsonb_build_object('intent_id', 'int_synth01', 'correlation_id', 'corr_synth01', 'policy_version', '` + FixturePolicyVersion +
		`', 'intent_hash', '` + FixtureIntentHash + `', 'issued_at', '` + FixtureIssuedAt + `', 'expires_at', '` + FixtureRecordExpires + `',
		'authority', jsonb_build_object('technical', 'PASS', 'policy', 'PASS', 'business', 'PASS'))`
	// GrantBoundSQL is the same for a grant document: it agrees with BoundFieldsSQL and expires before it.
	GrantBoundSQL = `jsonb_build_object('intent_hash', '` + FixtureIntentHash + `', 'policy_version', '` + FixturePolicyVersion + `', 'expires_at', '2026-11-07T10:00:00.000Z')`
)

// DecisionDocJSON returns a bound-complete, SYNTHETIC Decision Record document for a decision that a stored grant refers to. The grant's own intent hash,
// policy version and correlation ID are carried so that the grant agrees with its record (ruling of 8 Nov 2026 on noema#329); the record lives longer than any
// grant a test issues.
func DecisionDocJSON(decisionID, tenant, intentHash, policyVersion, correlation string) string {
	doc := map[string]any{
		"decision_id": decisionID, "tenant_id": tenant, "outcome": "ALLOW", "sig": map[string]string{"key_id": "k1", "value": "aa"},
		"intent_id": "int_synth01", "correlation_id": correlation, "policy_version": policyVersion, "intent_hash": intentHash,
		"issued_at": FixtureIssuedAt, "expires_at": FixtureRecordExpires,
		"authority": map[string]string{"technical": "PASS", "policy": "PASS", "business": "PASS"},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		panic("testpg: " + err.Error()) // a map of strings cannot fail to marshal
	}
	return string(raw)
}
