package orchestrator_test

// STM-01, ruling A205 (Q5, relayed, not yet committed), INV-12, AGENT-10: the authenticated session carries the caller's role, and the orchestrator hands the session to
// every port exactly as it received it, role included, so Trust's technical-authority step (T6) reads the role from the session and from nothing else. The orchestrator
// itself interprets no role: an empty role is the edge's and Trust's to refuse, not the runner's. All data is SYNTHETIC.

import (
	"context"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator"
)

// seen records the session each port was called with, by port. The shared fakes keep only the last call's session, so a port that received a stripped session
// could still pass a check on them (Security Reviewer, note 1 on noema#560).
type seen struct {
	*fakes
	by map[string]orchestrator.Session
}

func (w *seen) Lookup(ctx context.Context, s orchestrator.Session, ref string) (orchestrator.Admission, error) {
	w.by["admissions"] = s
	return w.fakes.Lookup(ctx, s, ref)
}
func (w *seen) Interpret(ctx context.Context, s orchestrator.Session, t orchestrator.Turn) (orchestrator.Interpretation, error) {
	w.by["interpreter"] = s
	return w.fakes.Interpret(ctx, s, t)
}
func (w *seen) Read(ctx context.Context, s orchestrator.Session, ref string) (orchestrator.EvidenceRef, error) {
	w.by["evidence"] = s
	return w.fakes.Read(ctx, s, ref)
}
func (w *seen) Decide(ctx context.Context, s orchestrator.Session, ref string, e orchestrator.EvidenceRef) (orchestrator.Decision, error) {
	w.by["trust"] = s
	return w.fakes.Decide(ctx, s, ref, e)
}
func (w *seen) Explain(ctx context.Context, s orchestrator.Session, id string) (orchestrator.Explanation, error) {
	w.by["explainer"] = s
	return w.fakes.Explain(ctx, s, id)
}
func (w *seen) AppendChange(ctx context.Context, s orchestrator.Session, c orchestrator.Change) error {
	w.by["recorder"] = s
	return w.fakes.AppendChange(ctx, s, c)
}

func TestTheSessionsRoleReachesEveryPortUnchanged(t *testing.T) {
	w := &seen{fakes: happy(), by: map[string]orchestrator.Session{}}
	tab, err := orchestrator.LoadTable()
	if err != nil {
		t.Fatal(err)
	}
	r, err := orchestrator.NewRunner(&orchestrator.Machine{Table: tab}, orchestrator.Ports{Interpreter: w, Evidence: w, Trust: w, Explainer: w, Recorder: w, Admissions: w})
	if err != nil {
		t.Fatal(err)
	}
	s := orchestrator.Session{TenantID: "ten_aa", PrincipalID: "usr_01", Role: "customer"}
	if _, err := r.RunRead(context.Background(), s, turn); err != nil {
		t.Fatal(err)
	}
	for _, port := range []string{"admissions", "interpreter", "evidence", "trust", "explainer", "recorder"} {
		if got, ok := w.by[port]; !ok || got != s {
			t.Errorf("the %s port saw %+v (called: %v), want the session as given, role included: %+v", port, got, ok, s)
		}
	}
}

// A session with no role is not refused by the runner: Trust refuses it at T6 (fail closed there), and a read-only run for a caller the edge has verified must not be
// stopped by a field the orchestrator does not interpret.
func TestASessionWithoutARoleIsPassedOnAsItIs(t *testing.T) {
	f := happy()
	if _, err := runner(t, f).RunRead(context.Background(), sess, turn); err != nil || f.session.Role != "" {
		t.Fatalf("err %v, role %q; want the session passed on with its role empty", err, f.session.Role)
	}
}
