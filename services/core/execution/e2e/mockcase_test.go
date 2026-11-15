package e2e_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// syncBuffer is a bytes.Buffer that the writer goroutine of os/exec and a reader (the harness, waiting for the process's first log line) may use at once.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// listeningMsg is the log line mock-case writes once both listeners are bound (cmd/mock-case/main.go).
// listeningMsg is the message of the line synthetic/mock_case/cmd/mock-case/main.go logs once both listeners are bound (the addresses actually bound, not the ones asked for). It is a
// contract between this harness and that binary: if the text changes there, every harness waits a minute and fails with the process log (a clear failure, not a flake).
const listeningMsg = "mock-case listening (SYNTHETIC)"

// parseListening reads one log line and returns the case and bench addresses the process bound, if the line is the one that reports them. The harness starts the
// process on port 0 so that the process itself holds its listeners from the first instant: no port is chosen before it is bound, so another process cannot win it
// in between (noema#525). Both addresses must be loopback: a listener on any other interface is refused.
func parseListening(line string) (caseAddr, benchAddr string, ok bool, err error) {
	var rec struct {
		Msg   string `json:"msg"`
		Case  string `json:"case"`
		Bench string `json:"bench"`
	}
	if json.Unmarshal([]byte(line), &rec) != nil || rec.Msg != listeningMsg {
		return "", "", false, nil
	}
	for _, a := range []string{rec.Case, rec.Bench} {
		host, port, splitErr := net.SplitHostPort(a)
		if splitErr != nil || host != "127.0.0.1" || port == "" || port == "0" {
			return "", "", false, fmt.Errorf("mock-case reported the address %q: want 127.0.0.1 and a bound port", a)
		}
	}
	return rec.Case, rec.Bench, true, nil
}

// boundAddrs waits for the process to report the addresses it bound. If the process exits first it fails at once with its log (not after a timeout).
func boundAddrs(t testing.TB, logs *syncBuffer, exited <-chan struct{}) (caseAddr, benchAddr string) {
	t.Helper()
	deadline := time.After(time.Minute)
	for {
		for _, line := range strings.Split(logs.String(), "\n") {
			c, b, ok, err := parseListening(strings.TrimSpace(line))
			if err != nil {
				t.Fatalf("%v; log:\n%s", err, logs.String())
			}
			if ok {
				return c, b
			}
		}
		select {
		case <-exited:
			t.Fatalf("mock-case stopped before it reported its addresses; log:\n%s", logs.String())
		case <-deadline:
			t.Fatalf("mock-case did not report its addresses within a minute; log:\n%s", logs.String())
		case <-time.After(20 * time.Millisecond):
		}
	}
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

// ensureMockCaseRoles creates the two cluster-wide roles the mock-case needs, if they do not exist. Roles are shared by every test
// binary on the server, so a check followed by a create races across processes: the create takes an advisory lock and tolerates a
// role made by another caller in the meantime.
func ensureMockCaseRoles(ctx context.Context, conn *pgx.Conn, admin, app string) error {
	_, err := conn.Exec(ctx, `
DO $$ BEGIN
  PERFORM pg_advisory_lock(7200310001);
  BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '`+admin+`') THEN
      CREATE ROLE `+admin+` LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD '`+rolePassword+`';
    END IF;
  EXCEPTION WHEN duplicate_object OR unique_violation THEN NULL;
  END;
  BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '`+app+`') THEN
      CREATE ROLE `+app+` LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD '`+rolePassword+`';
    END IF;
  EXCEPTION WHEN duplicate_object OR unique_violation THEN NULL;
  END;
  PERFORM pg_advisory_unlock(7200310001);
END $$`)
	return err
}

// mockCase is a running mock-case: the address of its case listener and the runtime credential file the core's case
// client reads. It holds no other handle on it.
type mockCase struct {
	URL            string
	TokenFile      string
	BenchURL       string // the control and truth listener, for the test-only bench client (evidence_faults_test.go)
	BenchTokenFile string
}

// healthClient bounds each readiness probe, so a process that accepted the connection and then hangs cannot stall the wait loop.
var healthClient = &http.Client{Timeout: 5 * time.Second}

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
	err = ensureMockCaseRoles(ctx, conn, "mock_case_admin", "mock_case_app")
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
	var logs syncBuffer
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(),
		"MOCK_CASE_LISTEN=127.0.0.1:0", // port 0: the process binds its own free port and logs it; the harness never chooses one (noema#525)
		"MOCK_CASE_BENCH_LISTEN=127.0.0.1:0",
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
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-exited
		if t.Failed() {
			t.Logf("mock-case log:\n%s", logs.String())
		}
	})
	caseAddr, otherAddr := boundAddrs(t, &logs, exited)
	wait, stopWaiting := context.WithTimeout(context.Background(), time.Minute)
	defer stopWaiting()
	for {
		resp, err := healthClient.Get("http://" + caseAddr + "/healthz")
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
	return mockCase{URL: "http://" + caseAddr, TokenFile: runtimeTokenFile, BenchURL: "http://" + otherAddr, BenchTokenFile: benchTokenFile}
}
