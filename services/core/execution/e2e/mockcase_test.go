package e2e_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
)

// This file starts the real mock-case binary (synthetic/mock_case) against its own real PostgreSQL database. The core
// holds the runtime credential only; the other credential mock-case requires is written to a file and never used by
// any code here, and nothing in this package calls the truth or control endpoints (they belong to the benchmark runner).

const rolePassword = "test-only-not-a-secret" // SYNTHETIC: the roles exist only on the throwaway test server

var (
	buildOnce sync.Once
	buildPath string
	buildErr  error
	dbCount   int
	dbMu      sync.Mutex
)

func repoRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "synthetic", "mock_case", "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("cannot find synthetic/mock_case above the test's directory")
		}
		dir = parent
	}
}

func buildMockCase(t testing.TB) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "mock-case-bin-")
		if err != nil {
			buildErr = err
			return
		}
		buildPath = filepath.Join(dir, "mock-case")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "build", "-o", buildPath, "./cmd/mock-case")
		cmd.Dir = filepath.Join(repoRoot(t), "synthetic", "mock_case")
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("go build mock-case: %v: %s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return buildPath
}

func freePort(t testing.TB) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return port
}

func writeSecret(t testing.TB, dir, name, value string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(value+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func randomHex(t testing.TB) string {
	t.Helper()
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b[:])
}

// mockCase is a running mock-case: the address of its case listener and the runtime credential file the core's case
// client reads. It holds no other handle on it.
type mockCase struct {
	URL       string
	TokenFile string
}

func startMockCase(t *testing.T) mockCase {
	t.Helper()
	bin := buildMockCase(t)
	server := testpg.ServerURL(t)
	u, err := url.Parse(server)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, server)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)

	dbMu.Lock()
	dbCount++
	name := fmt.Sprintf("mock_case_e2e_%d_%d", os.Getpid(), dbCount)
	_, err = conn.Exec(ctx, `
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mock_case_admin') THEN
    CREATE ROLE mock_case_admin LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD '`+rolePassword+`';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mock_case_app') THEN
    CREATE ROLE mock_case_app LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD '`+rolePassword+`';
  END IF;
END $$`)
	dbMu.Unlock()
	if err != nil {
		t.Fatalf("create mock-case roles: %v", err)
	}
	for _, q := range []string{
		`CREATE DATABASE ` + name + ` OWNER mock_case_admin`,
		`REVOKE ALL ON DATABASE ` + name + ` FROM PUBLIC`,
		`GRANT CONNECT ON DATABASE ` + name + ` TO mock_case_app`,
	} {
		if _, err := conn.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if cc, err := pgx.Connect(c, server); err == nil {
			_, _ = cc.Exec(c, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
			_ = cc.Close(c)
		}
	})

	secrets := t.TempDir()
	runtimeTokenFile := writeSecret(t, secrets, "runtime_token", randomHex(t))
	benchTokenFile := writeSecret(t, secrets, "other_token", randomHex(t))
	caseAddr := "127.0.0.1:" + freePort(t)
	otherAddr := "127.0.0.1:" + freePort(t)
	var logs bytes.Buffer
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(),
		"MOCK_CASE_LISTEN="+caseAddr,
		"MOCK_CASE_BENCH_LISTEN="+otherAddr,
		"MOCK_CASE_DB_HOST="+u.Hostname(), "MOCK_CASE_DB_PORT="+u.Port(), "MOCK_CASE_DB_NAME="+name,
		"MOCK_CASE_DB_APP_PASSWORD_FILE="+writeSecret(t, secrets, "app_pw", rolePassword),
		"MOCK_CASE_DB_ADMIN_PASSWORD_FILE="+writeSecret(t, secrets, "admin_pw", rolePassword),
		"MOCK_CASE_RUNTIME_TOKEN_FILE="+runtimeTokenFile,
		"MOCK_CASE_BENCH_TOKEN_FILE="+benchTokenFile,
	)
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("start mock-case: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("mock-case log:\n%s", logs.String())
		}
	})
	wait, stopWaiting := context.WithTimeout(context.Background(), time.Minute)
	defer stopWaiting()
	for {
		resp, err := http.Get("http://" + caseAddr + "/healthz")
		if err == nil {
			ok := resp.StatusCode == http.StatusOK
			_ = resp.Body.Close()
			if ok {
				break
			}
		}
		if wait.Err() != nil {
			t.Fatalf("mock-case did not become healthy; log:\n%s", logs.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
	return mockCase{URL: "http://" + caseAddr, TokenFile: runtimeTokenFile}
}
