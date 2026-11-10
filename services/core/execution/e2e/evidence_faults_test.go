package e2e_test

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

	"github.com/Noemai-org/noema/services/core/adapters/synthetic_tushe"
	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// EVD-01 slice 2c, G1 story: evidence built from a read that the REAL mock-case faults on, with FaultPlans loaded through the test-only bench
// client (AGENT-12: no mocked failure mode). REQ-CTX-001, REQ-CTX-002, REQ-AUD-001, INV-19, EC-S11. All data is SYNTHETIC.

const faultBase = "../../../../contracts/fixtures/valid/fault-plan/mock_case_commit_then_drop.json"

// benchClient is the test-only control client of DEC-2026-10-26 Amendment 1; it lives in _test.go files only.
type benchClient struct{ url, token string }

func newBenchClient(t testing.TB, mc mockCase) benchClient {
	t.Helper()
	b, err := os.ReadFile(mc.BenchTokenFile)
	if err != nil {
		t.Fatal(err)
	}
	return benchClient{url: mc.BenchURL, token: strings.TrimSpace(string(b))}
}

func (b benchClient) loadPlan(t testing.TB, plan []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, b.url+"/control/v1/fault-plan", bytes.NewReader(plan))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+b.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		t.Fatalf("load plan: %d %s", resp.StatusCode, out)
	}
}

func readFaultPlan(t testing.TB, id, effect string, params map[string]any) []byte {
	t.Helper()
	raw, err := os.ReadFile(faultBase)
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	plan["fault_plan_id"] = id
	plan["faults"] = []any{map[string]any{"target": "mock-case", "operation": "get_case", "match": map[string]any{"nth_call": 1}, "effect": effect, "params": params}}
	out, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestEvidenceFromARealFaultedReadIsNeverKnown(t *testing.T) {
	mc := startMockCase(t)
	bench := newBenchClient(t, mc)
	cases, err := synthetic_tushe.NewCaseClient(mc.URL, mc.TokenFile, nil)
	if err != nil {
		t.Fatal(err)
	}
	admin, dsn := testpg.NewDatabase(t)
	bg, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := db.Migrate(bg, admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(bg, `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Synthetic bank A')`, tenant); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewManual(time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)) // SYNTHETIC
	gen := ids.New(clk, nil)
	builder := &layer.Builder{Pool: testpg.TenantLogin(t, admin, dsn, "evidence_rw", tenant), Clock: clk, IDs: gen, RollbackTimeout: 5 * time.Second, Rec: &chain.Appender{Clock: clk, IDs: gen}}

	steps := []struct {
		name      string
		plan      []byte
		timeout   time.Duration
		wantClass layer.ReadClass
		wantState string
		wantBody  bool // whether the real fault leaves response bytes for the adapter to hash
	}{
		{"HTTP_5XX 503", readFaultPlan(t, "fp_evd_read_5xx_v1", "HTTP_5XX", map[string]any{"status": 503}), 5 * time.Second, layer.ReadServerError, "UNAVAILABLE", true},
		{"HTTP_429 with Retry-After", readFaultPlan(t, "fp_evd_read_429_v1", "HTTP_429", map[string]any{"retry_after_seconds": 2}), 5 * time.Second, layer.ReadRateLimited, "UNAVAILABLE", true},
		{"TIMEOUT_BEFORE_COMMIT longer than the read's deadline", readFaultPlan(t, "fp_evd_read_timeout_v1", "TIMEOUT_BEFORE_COMMIT", map[string]any{"delay_ms": 4000}), time.Second, layer.ReadTimeout, "UNAVAILABLE", false},
		{"a real 404 for a case the target does not hold", nil, 5 * time.Second, layer.ReadNotFound, "UNKNOWN", true}, // absence is never a negative fact
	}
	for _, s := range steps {
		if s.plan != nil {
			bench.loadPlan(t, s.plan)
		}
		rctx, stop := context.WithTimeout(context.Background(), s.timeout)
		_, readErr := cases.GetCase(rctx, tenant, "cas_missing01")
		stop()
		class := layer.ClassifyError(readErr)
		if class != s.wantClass {
			t.Fatalf("%s: class = %s (err %v), want %s", s.name, class, readErr, s.wantClass)
		}
		rawHash, noBytes := layer.BodyOf(readErr) // the adapter's own record of the bytes it received (noema#262)
		o := layer.Observation{TenantID: tenant, CorrelationID: "corr_synthetic01", Source: "fin.src.core_case", SourceRecordID: "cas_missing01", ReadBy: "TRUST", RawHash: rawHash, NoBytes: noBytes,
			EffectiveAt: clk.Now(), AuthorityClass: "AUTHORITATIVE_FOR:case_status", ResourceScope: "res_synthetic", PrincipalScope: "prn_synthetic",
			Sensitivity: "S2_internal", Synthetic: true, Read: layer.Read{Class: class, ReadAt: clk.Now()}, Src: layer.Source{Authority: layer.AuthAuthoritative, SourceLimit: 300 * time.Second}}
		item, err := builder.Record(bg, o)
		if err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		if string(item.Status) != s.wantState {
			t.Errorf("%s: status = %s, want %s", s.name, item.Status, s.wantState)
		}
		// Provenance (Codex finding on noema#172): the Record says whether a body arrived, and a body that arrived is never stored as the hash of nothing.
		var received bool
		var stored string
		if err := admin.QueryRow(bg, `SELECT (e.payload->>'body_received')::boolean, p.raw_hash FROM record_event e JOIN evidence_item i ON i.evidence_id = e.payload->>'evidence_id'
			JOIN evidence_payload p ON p.tenant_id = i.tenant_id AND p.payload_ref = i.payload_ref WHERE e.payload->>'evidence_id' = $1`, item.EvidenceID).Scan(&received, &stored); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		if received == noBytes {
			t.Errorf("%s: the event says body_received = %v although the adapter said noBytes = %v", s.name, received, noBytes)
		}
		if received && stored != rawHash {
			t.Errorf("%s: stored raw hash %s, want the adapter's %s", s.name, stored, rawHash)
		}
		if s.wantBody != received {
			t.Errorf("%s: body_received = %v, want %v", s.name, received, s.wantBody)
		}
	}
	var events, known int
	if err := admin.QueryRow(bg, `SELECT count(*) FROM record_event WHERE event_type = 'EVIDENCE_READ'`).Scan(&events); err != nil || events != len(steps) {
		t.Fatalf("EVIDENCE_READ events = %d (%v), want %d", events, err, len(steps))
	}
	var labelled int // AGENT-25: evidence read from the synthetic mock-case is labelled synthetic on the Record
	if err := admin.QueryRow(bg, `SELECT count(*) FROM record_event WHERE event_type = 'EVIDENCE_READ' AND payload->>'synthetic' = 'true'`).Scan(&labelled); err != nil || labelled != len(steps) {
		t.Fatalf("EVIDENCE_READ events labelled synthetic = %d (%v), want %d", labelled, err, len(steps))
	}
	if err := admin.QueryRow(bg, `SELECT count(*) FROM evidence_item WHERE fact_status = 'KNOWN'`).Scan(&known); err != nil || known != 0 {
		t.Fatalf("%d items are KNOWN although every read was faulted or empty (%v)", known, err)
	}
	if _, err := chain.Verify(bg, admin, tenant); err != nil {
		t.Fatalf("the Record chain does not verify: %v", err)
	}
}
