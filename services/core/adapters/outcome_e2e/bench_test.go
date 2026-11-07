package outcomee2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// bench is the TEST-ONLY control and truth client of DEC-2026-10-26 Amendment 1. It exists in _test.go files of this
// package only; boundary_test.go fails if any other file under services/ names a control or truth path or the bench
// credential. It plays what the benchmark runner plays: it loads a FaultPlan, advances the simulated clock and reads the
// truth, so that a test can check what the target really holds. The runtime never calls these paths.
type bench struct {
	url   string
	token string
}

func newBench(t testing.TB, mc mockCase) bench {
	t.Helper()
	b, err := os.ReadFile(mc.BenchTokenFile)
	if err != nil {
		t.Fatal(err)
	}
	return bench{url: mc.BenchURL, token: strings.TrimSpace(string(b))}
}

func (b bench) do(t testing.TB, method, path string, body []byte) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, b.url+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+b.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode/100 != 2 {
		t.Fatalf("%s %s: %d %s", method, path, resp.StatusCode, out)
	}
	return out
}

// loadPlan loads a FaultPlan (the JSON of contracts/schemas/fault-plan.v1.json) into mock-case.
func (b bench) loadPlan(t testing.TB, plan []byte) {
	b.do(t, http.MethodPut, "/control/v1/fault-plan", plan)
}

// advance applies the plan's named clock step (for example after_receipt).
func (b bench) advance(t testing.TB, step string) {
	body, _ := json.Marshal(map[string]string{"step": step})
	b.do(t, http.MethodPost, "/control/v1/clock/advance", body)
}

type truthCase struct {
	CaseID          string `json:"case_id"`
	IdempotencyKey  string `json:"idempotency_key"`
	TenantID        string `json:"tenant_id"`
	TransactionRef  string `json:"transaction_ref"`
	Status          string `json:"status"`
	CreatedByIntent string `json:"created_by_intent"`
}

// cases is every case the target holds, read from the truth endpoint.
func (b bench) cases(t testing.TB) []truthCase {
	var s struct {
		Cases []truthCase `json:"cases"`
	}
	if err := json.Unmarshal(b.do(t, http.MethodGet, "/truth/v1/state", nil), &s); err != nil {
		t.Fatal(err)
	}
	return s.Cases
}

type command struct {
	Operation          string  `json:"operation"`
	IdempotencyKey     *string `json:"idempotency_key"`
	StatusCode         int     `json:"status_code"`
	Committed          bool    `json:"committed"`
	ResponseSuppressed bool    `json:"response_suppressed"`
	FaultEffect        *string `json:"fault_effect"`
}

// commands is the target's command log: every command it received, with what became of it.
func (b bench) commands(t testing.TB) []command {
	var c struct {
		Commands []command `json:"commands"`
		Complete bool      `json:"complete"`
	}
	if err := json.Unmarshal(b.do(t, http.MethodGet, "/truth/v1/commands?limit=1000", nil), &c); err != nil || !c.Complete {
		t.Fatalf("command log: %v (complete %v)", err, c.Complete)
	}
	return c.Commands
}

// creates counts the create commands the target received under a key, committed or not.
func creates(cmds []command, key string) (received, committed int) {
	for _, c := range cmds {
		if c.Operation == "create_case" && c.IdempotencyKey != nil && *c.IdempotencyKey == key {
			received++
			if c.Committed {
				committed++
			}
		}
	}
	return received, committed
}
