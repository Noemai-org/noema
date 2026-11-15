package standin_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"testing"
)

// The exchanges the core's StandinClient is tested against (task ENV-06; REQ-ENV-001, REQ-ENV-005; AGENT-12).
//
// Each exchange is the REAL stand-in service, over a real connection, loaded with the golden fixture and read as its own principal.
// Nothing is written by hand: the client is tested on what the service really sends, byte for byte (the client hashes the wire bytes).
// The file is deterministic (the service's clock is fixed); this test regenerates it in memory and fails if the committed copy
// differs, so a change of the service cannot leave the client's tests asserting an older reading. To re-record:
//
//	NOEMA_RECORD_STANDIN=1 go test ./internal/standin -run TestStandinRecordings
//
// The faulted copies (HTTP 5xx, 429, the corrupted-payload families, pagination) are recorded from the real hostility proxy in front
// of these exchanges: benchmark/hostility_proxy/internal/proxy/record_standin_test.go. SYNTHETIC (AGENT-25).
const (
	recordingsFile = "../../../../services/core/adapters/synthetic_tushe/testdata/standin_recordings.json"
	recordEnv      = "NOEMA_RECORD_STANDIN"
)

type recordedExchange struct {
	Scenario  string `json:"scenario"`
	Principal string `json:"principal"`
	Request   struct {
		Path  string `json:"path"`
		Query string `json:"query"`
	} `json:"request"`
	Response struct {
		Status      int    `json:"status"`
		ContentType string `json:"content_type"`
		Body        string `json:"body"`
	} `json:"response"`
}

type recordedFile struct {
	Provenance string             `json:"provenance"`
	Exchanges  []recordedExchange `json:"exchanges"`
}

var recordedReads = []struct {
	scenario, principal, path, query string
}{
	{"ledger_posted", "usr_a", "/standin/v1/transactions/fin.transaction:TX1/ledger-entries", ""},
	{"ledger_unavailable", "usr_a", "/standin/v1/transactions/fin.transaction:TX3/ledger-entries", ""},
	{"switch_accepted", "usr_a", "/standin/v1/transactions/fin.transaction:TX1/switch-status", ""},
	{"switch_timed_out", "usr_a", "/standin/v1/transactions/fin.transaction:TX2/switch-status", ""},
	{"destination_pending", "usr_a", "/standin/v1/transactions/fin.transaction:TX2/destination-status", ""},
	{"destination_credited", "usr_a", "/standin/v1/transactions/fin.transaction:TX1/destination-status", ""},
	{"candidates_of_usr_a", "usr_a", "/standin/v1/transactions", "account=acc_usr_a"},
	{"another_principals_transaction", "usr_a", "/standin/v1/transactions/fin.transaction:TX9/ledger-entries", ""},
}

func recordStandin(t *testing.T) []byte {
	t.Helper()
	out := recordedFile{Provenance: "Recorded from the real synthetic/standin service loaded with testdata/derive-golden.json, fixed clock 2026-10-01T09:40:00Z; SYNTHETIC. Regenerate with " + recordEnv + "=1."}

	// Before anything is loaded every read is 503 STANDIN_NOT_LOADED, never an empty answer.
	empty := newRig(t)
	out.Exchanges = append(out.Exchanges, exchange(t, empty, "not_loaded", "usr_a", "/standin/v1/transactions/fin.transaction:TX1/ledger-entries", ""))

	loaded := newRig(t)
	if put := do(t, http.MethodPut, loaded.control.URL+"/control/v1/standin", benchToken, "", loaded.golden); put.status != 200 {
		t.Fatalf("load: %d %s", put.status, put.body)
	}
	for _, r := range recordedReads {
		out.Exchanges = append(out.Exchanges, exchange(t, loaded, r.scenario, r.principal, r.path, r.query))
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

func exchange(t *testing.T, r *rig, scenario, principal, path, query string) recordedExchange {
	t.Helper()
	target := r.data.URL + path
	if query != "" {
		target += "?" + query
	}
	if _, err := url.Parse(target); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+runtimeToken)
	req.Header.Set("X-Principal-Assertion", assertion(principal))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(res.Body); err != nil {
		t.Fatal(err)
	}
	e := recordedExchange{Scenario: scenario, Principal: principal}
	e.Request.Path, e.Request.Query = path, query
	e.Response.Status, e.Response.ContentType, e.Response.Body = res.StatusCode, res.Header.Get("Content-Type"), buf.String()
	return e
}

func TestStandinRecordings(t *testing.T) {
	got := recordStandin(t)
	if os.Getenv(recordEnv) != "" {
		if err := os.WriteFile(recordingsFile, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("re-recorded %s", recordingsFile)
		return
	}
	want, err := os.ReadFile(recordingsFile)
	if err != nil {
		t.Fatalf("%v (record it with %s=1)", err, recordEnv)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
		t.Fatalf("the committed stand-in recordings are stale: re-record with %s=1 go test ./internal/standin -run TestStandinRecordings", recordEnv)
	}
}
