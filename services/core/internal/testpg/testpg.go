// Package testpg starts a throwaway PostgreSQL server for tests that need a real database (REQ-TST-004:
// Testcontainers or a real server, never an in-memory fake). Tests only; no production code imports it.
//
// If NOEMA_TEST_DATABASE_URL is set it is used as it is (CI may point it at a service container). Otherwise a
// server is started from the PostgreSQL binaries on the machine, on a private directory and a free port, and
// removed when the test binary exits. When neither exists the test fails: it never silently skips.
package testpg

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

// NewDatabase creates an empty database on the shared server and returns its pool, connected as the
// superuser (the migrating role). The database is dropped when the test ends.
func NewDatabase(t testing.TB) (*pgxpool.Pool, string) {
	t.Helper()
	once.Do(func() { baseURL, baseErr = start() })
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
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
