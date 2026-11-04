package api_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// AT-14, "Concurrent update" (Tushe specification v1.0 §22: "Modify a resource between read and mutation." Pass: "Stale/concurrent
// mutation detected as documented."). The worksheet of 13 Oct 2026 assigns it to Noema: the write target of the POC is a case system
// Noema runs itself, so version tokens and stale-write detection live in mock-case.
//
// This test is the recorded, seeded run. It runs the scenario from a fault plan (seed 42), twice on two fresh databases, requires the
// two runs to be identical, and requires the committed record synthetic/at_runs/AT-14.json to equal them (the drift guard). To
// re-record: NOEMA_RECORD_AT=1 go test ./internal/api -run TestAT14. SYNTHETIC (AGENT-25). Only structural facts are recorded; the
// generated identifiers stay out of the steps.

const (
	at14File = "../../../at_runs/AT-14.json"
	atEnv    = "NOEMA_RECORD_AT"
)

type atStep struct {
	Step                string `json:"step"`
	Request             string `json:"request"`
	Status              int    `json:"status"`
	ErrorCode           string `json:"error_code,omitempty"`
	VersionToken        string `json:"version_token,omitempty"`
	CurrentVersionToken string `json:"current_version_token,omitempty"`
	OwnerQueue          string `json:"owner_queue,omitempty"`
}

type atRun struct {
	Synthetic bool     `json:"synthetic"`
	Test      string   `json:"test"`
	Spec      string   `json:"spec"`
	Source    string   `json:"source"`
	Seed      int      `json:"seed"`
	PlanID    string   `json:"fault_plan_id"`
	Steps     []atStep `json:"steps"`
	Digest    struct {
		State      string `json:"state_hash"`
		FaultTrace string `json:"fault_trace_hash"`
	} `json:"digest"`
	Verdict string `json:"verdict"`
}

func at14Step(name, request string, r resp) atStep {
	s := atStep{Step: name, Request: request, Status: r.status, ErrorCode: r.errCode(), VersionToken: r.str("version_token"), OwnerQueue: r.str("owner_queue")}
	// the mismatch names the current token in the error's details: {"field":"current_version_token","issue":"v2"}
	if errBody, ok := r.json["error"].(map[string]any); ok {
		details, _ := errBody["details"].([]any)
		for _, d := range details {
			if m, ok := d.(map[string]any); ok && m["field"] == "current_version_token" {
				s.CurrentVersionToken, _ = m["issue"].(string)
			}
		}
	}
	return s
}

func runAT14(t *testing.T) atRun {
	t.Helper()
	e := newEnv(t)
	e.loadPlan(plan(fault("route_case", `{"nth_call":1}`, "CONCURRENT_MODIFICATION", `{}`), simClock)) // the simulated clock keeps real timestamps out of the state digest
	run := atRun{Synthetic: true, Test: "AT-14 Concurrent update", Source: "mock-case, fault CONCURRENT_MODIFICATION on route_case (nth_call 1), simulated clock", Seed: 42, PlanID: "fp_test",
		Spec: "Modify a resource between read and mutation. Pass: stale/concurrent mutation detected as documented."}
	created := e.createCase("idk_at14")
	id := created.str("case_id")
	run.Steps = append(run.Steps, at14Step("create the case", "POST /v1/cases", created))
	read1 := e.runtime("GET", "/v1/cases/"+id+"?tenant_id=ten_a", nil, nil)
	run.Steps = append(run.Steps, at14Step("read: the caller learns version v1", "GET /v1/cases/{case_id}", read1))
	route := "/v1/cases/" + id + "/route"
	stale := e.runtime("POST", route, nil, map[string]any{"tenant_id": "ten_a", "owner_queue": "DISPUTES", "expected_version_token": read1.str("version_token")})
	run.Steps = append(run.Steps, at14Step("mutate on v1 after another party modified the case: detected", "POST /v1/cases/{case_id}/route", stale))
	read2 := e.runtime("GET", "/v1/cases/"+id+"?tenant_id=ten_a", nil, nil)
	run.Steps = append(run.Steps, at14Step("read again: the competing modification is real, the mutation was not applied", "GET /v1/cases/{case_id}", read2))
	fresh := e.runtime("POST", route, nil, map[string]any{"tenant_id": "ten_a", "owner_queue": "DISPUTES", "expected_version_token": read2.str("version_token")})
	run.Steps = append(run.Steps, at14Step("mutate on the current version: applied", "POST /v1/cases/{case_id}/route", fresh))
	again := e.runtime("POST", route, nil, map[string]any{"tenant_id": "ten_a", "owner_queue": "DISPUTES", "expected_version_token": read2.str("version_token")})
	run.Steps = append(run.Steps, at14Step("repeat the mutation on the old version: still detected", "POST /v1/cases/{case_id}/route", again))
	run.Digest.State, run.Digest.FaultTrace = e.digest()

	// the pass condition, from the exchanges themselves
	ok := stale.status == 409 && stale.errCode() == "VERSION_MISMATCH" && run.Steps[2].CurrentVersionToken == "v2" &&
		read2.str("version_token") == "v2" && read2.str("owner_queue") == "PAYMENTS_OPERATIONS" &&
		fresh.status == 200 && fresh.str("version_token") == "v3" && fresh.str("owner_queue") == "DISPUTES" &&
		again.status == 409 && again.errCode() == "VERSION_MISMATCH" && run.Steps[5].CurrentVersionToken == "v3"
	run.Verdict = map[bool]string{true: "PASS", false: "FAIL"}[ok]
	return run
}

func renderAT(t *testing.T, run atRun) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(run); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestAT14RecordedSeededRun(t *testing.T) {
	first := runAT14(t)
	second := runAT14(t)
	if first.Verdict != "PASS" {
		t.Fatalf("AT-14 does not hold: %s", renderAT(t, first))
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("the same plan and seed must give the same run:\n%s\n%s", renderAT(t, first), renderAT(t, second))
	}
	got := renderAT(t, first)
	if os.Getenv(atEnv) != "" {
		if err := os.WriteFile(at14File, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", at14File)
		return
	}
	want, err := os.ReadFile(at14File)
	if err != nil {
		t.Fatalf("%v: record it with %s=1", err, atEnv)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("synthetic/at_runs/AT-14.json is stale: re-record with %s=1 go test ./internal/api -run TestAT14\nrecorded:\n%s\nnow:\n%s", atEnv, want, got)
	}
}
