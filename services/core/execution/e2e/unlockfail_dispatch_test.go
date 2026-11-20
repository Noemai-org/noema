package e2e_test

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/bridge"
)

type lineSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *lineSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *lineSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Count(s.buf.String(), "\n")
}

// noema#748, Architect ruling A348 as refined (relayed; interim), REQ-EXE-001, REQ-EXE-002, INV-03, INV-07: a spend that committed and whose unlock failed is still a spend. The
// controller dispatches exactly once, the write reaches the real mock-case once, the receipt is signed and the grant is SPENT; the caller sees no error. The unlock fails on the
// real database (EXECUTE on pg_advisory_unlock revoked). SYNTHETIC.
func TestAGrantWhoseSpendCommittedAndWhoseUnlockFailedIsStillDispatchedOnce(t *testing.T) {
	mc := startMockCase(t)
	st := newStack(t, mc)
	sink := &lineSink{}
	st.led.Log = slog.New(slog.NewJSONHandler(sink, nil))
	if _, err := st.admin.Exec(bg(t), `REVOKE EXECUTE ON FUNCTION pg_advisory_unlock(int4, int4) FROM PUBLIC`); err != nil {
		t.Fatal(err)
	}
	createGrant, err := st.trust.IssueCreate(bg(t))
	if err != nil {
		t.Fatal(err)
	}
	out, err := bridge.GrantExecutor{Controller: st.ctl}.ExecuteGrant(bg(t), tenant, createGrant)
	if err != nil {
		t.Fatalf("ExecuteGrant error = %v; the spend committed, so the grant is dispatched and no error is returned", err)
	}
	if out.ReceiptID == "" || out.Replayed || out.Void != "" {
		t.Fatalf("outcome %+v; want a receipt of a dispatched write", out)
	}
	if got := st.count(t, `SELECT count(*) FROM decision_grant WHERE grant_id = $1 AND status = 'SPENT'`, createGrant); got != 1 {
		t.Fatalf("the grant is not SPENT (%d)", got)
	}
	if got := st.count(t, `SELECT count(*) FROM grant_spend WHERE grant_id = $1`, createGrant); got != 1 {
		t.Fatalf("%d spends of the grant, want 1", got)
	}
	if got := st.count(t, `SELECT count(*) FROM record_event WHERE tenant_id = $1 AND event_type = 'DISPATCH_ATTEMPTED'`, tenant); got != 1 {
		t.Fatalf("%d DISPATCH_ATTEMPTED events, want 1", got)
	}
	if n := sink.count(); n != 1 {
		t.Fatalf("%d log lines, want exactly the one line of the failed release", n)
	}
}
