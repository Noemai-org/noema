package recovery_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
	"github.com/Noemai-org/noema/services/core/orchestrator/recovery"
	"pgregory.net/rapid"
)

func outcomeOf(r recovery.Report, id string) recovery.Outcome {
	for _, i := range r.Items {
		if i.InvestigationID == id {
			return i.Outcome
		}
	}
	return ""
}

// REQ-STM-001, REQ-RCV-003, INV-07, AGENT-07, AGENT-08, A204: the process dies after the grant was spent and before the verdict. Recovery verifies by the grant's key and
// never dispatches again, whatever executor the recovery process is wired with, and it does not ask Trust again.
func TestRecoverAfterSpendBeforeVerifyNeverDispatchesAgain(t *testing.T) {
	for _, tc := range []struct {
		name  string
		crash func(*world)
		stuck orchestrator.State
	}{
		{"killed after the spend, before EXECUTING is recorded", func(w *world) { w.crashExec = true }, "PLANNED"},
		{"killed when asked to verify", func(w *world) { w.crashVerify = true }, "AWAITING_VERIFICATION"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld()
			tc.crash(w)
			if err := w.firstProcess(t); !errors.Is(err, errCrash) {
				t.Fatalf("the first process should die at the crash point, got %v", err)
			}
			if w.state() != tc.stuck {
				t.Fatalf("the stored state is %s, want %s", w.state(), tc.stuck)
			}
			w.crashExec, w.crashVerify = false, false
			decide, execute, _ := w.counts()
			rep, err := w.activity(t, w).Recover(tctx(), tenantA) // the recovery process holds a real, counting executor on purpose
			if err != nil {
				t.Fatal(err)
			}
			if got := outcomeOf(rep, invID); got != recovery.Resumed {
				t.Fatalf("outcome %q, want %q", got, recovery.Resumed)
			}
			d2, e2, v2 := w.counts()
			if e2 != execute {
				t.Fatalf("the grant was dispatched again: %d executes, was %d", e2, execute)
			}
			if d2 != decide {
				t.Fatal("recovery asked Trust again for a decision")
			}
			if v2 < 1 || w.state() != "COMPLETED" {
				t.Fatalf("the grant was not verified by key to completion: verifies %d, state %s", v2, w.state())
			}
		})
	}
}

// REQ-RCV-003, INV-07, A224: an UNKNOWN or COMPENSATED verdict stops for a person, and a later pass neither verifies nor dispatches again.
func TestRecoverUnknownOrCompensatedStopsForAPerson(t *testing.T) {
	for _, v := range []contracts.EnumsV1Verdict{contracts.EnumsV1VerdictUNKNOWN, contracts.EnumsV1VerdictCOMPENSATED} {
		t.Run(string(v), func(t *testing.T) {
			w := newWorld()
			w.crashVerify = true
			_ = w.firstProcess(t)
			w.crashVerify, w.verdicts = false, []contracts.EnumsV1Verdict{v}
			a := w.activity(t, recovery.NoDispatch{})
			for pass := 0; pass < 2; pass++ {
				rep, err := a.Recover(tctx(), tenantA)
				if err != nil {
					t.Fatal(err)
				}
				if got := outcomeOf(rep, invID); got != recovery.NeedsPerson {
					t.Fatalf("pass %d: outcome %q, want %q", pass, got, recovery.NeedsPerson)
				}
			}
			_, execute, verify := w.counts()
			if execute != 1 || verify != 2 { // 1 from the first process's spend; the verify by key of the first pass (the crashed ask is counted) only
				t.Fatalf("executes %d, verifies %d: a stopped investigation must not be verified or dispatched again", execute, verify)
			}
			if w.state() != "RECOVERING" {
				t.Fatalf("state %s, want RECOVERING", w.state())
			}
		})
	}
}

// INV-03, AGENT-08: a grant the Record does not show as spent is never dispatched by recovery; the investigation stops for a person and the grant expires by its TTL.
func TestRecoverNeverSpendsAnUnspentGrant(t *testing.T) {
	w := newWorld()
	w.rows[invID].inv = orchestrator.Investigation{ID: invID, State: "PLANNED", Version: 3}
	w.rows[invID].decision = orchestrator.Decision{ID: decisionID, Outcome: contracts.EnumsV1DecisionOutcomeALLOW, GrantIDs: []string{grantID}, IntentRef: intentRef}
	rep, err := w.activity(t, w).Recover(tctx(), tenantA)
	if err != nil {
		t.Fatal(err)
	}
	if got := outcomeOf(rep, invID); got != recovery.NeedsPerson {
		t.Fatalf("outcome %q, want %q", got, recovery.NeedsPerson)
	}
	if _, execute, verify := w.counts(); execute != 0 || verify != 0 {
		t.Fatalf("executes %d, verifies %d: nothing may run for an unspent grant", execute, verify)
	}
}

// PERD 32.4, EC-E16: the Record comes first. When the move cannot be recorded nothing is verified.
func TestRecoverRecordFirst(t *testing.T) {
	w := newWorld()
	w.crashExec = true
	_ = w.firstProcess(t)
	w.crashExec, w.recordEr = false, errors.New("the Record is down")
	rep, err := w.activity(t, recovery.NoDispatch{}).Recover(tctx(), tenantA)
	if err != nil {
		t.Fatal(err)
	}
	if got := outcomeOf(rep, invID); got != recovery.Failed {
		t.Fatalf("outcome %q, want %q", got, recovery.Failed)
	}
	if _, execute, verify := w.counts(); execute != 1 || verify != 0 {
		t.Fatalf("executes %d, verifies %d: nothing may follow a failed append", execute, verify)
	}
}

// REQ-SEC-004, INV-12: the pass is scoped to the tenant it was given. A row of another tenant that a reader returned is refused before any port is called, and the
// reader is asked for the given tenant, the configured idle age and the configured batch.
func TestRecoverIsTenantScoped(t *testing.T) {
	w := newWorld()
	w.crashExec = true
	_ = w.firstProcess(t)
	w.crashExec, w.batchIgnored = false, true
	w.rows[invID].tenant = tenantB // the reader now hands back tenant B's row when asked for A
	rep, err := w.activity(t, recovery.NoDispatch{}).Recover(tctx(), tenantA)
	if err != nil {
		t.Fatal(err)
	}
	if got := outcomeOf(rep, invID); got != recovery.Refused {
		t.Fatalf("outcome %q, want %q", got, recovery.Refused)
	}
	if _, _, verify := w.counts(); verify != 0 {
		t.Fatal("a port ran for another tenant's row")
	}
	if len(w.readerCalls) != 1 || w.readerCalls[0] != (readerCall{tenantA, 60000, 10}) {
		t.Fatalf("the reader was asked %+v", w.readerCalls)
	}
	if _, err := w.activity(t, recovery.NoDispatch{}).Recover(tctx(), "ten_x'; DROP"); err == nil {
		t.Fatal("a malformed tenant must be refused")
	}
}

// A bounded batch: a reader that returns more rows than the limit does not make the pass process more.
func TestRecoverBatchIsBounded(t *testing.T) {
	w := newWorld()
	w.batchIgnored = true
	for _, id := range []string{"inv_BB01", "inv_BB02", "inv_BB03"} {
		w.rows[id] = &row{tenant: tenantA, principal: principal, corr: "corr_" + id[4:], recev: "recev_" + id[4:], inv: orchestrator.Investigation{ID: id, State: "RECOVERING", Version: 5}}
	}
	a, err := recovery.New(recovery.Config{IdleMS: 1000, Batch: 2, RunTimeoutMS: 500}, w, w, w, w, w.runner(t, recovery.NoDispatch{}))
	if err != nil {
		t.Fatal(err)
	}
	rep, err := a.Recover(tctx(), tenantA)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Items) != 2 {
		t.Fatalf("processed %d, the batch is 2", len(rep.Items))
	}
}

// Single flight per correlation: while one pass verifies an investigation, a second pass skips it and never verifies it too.
func TestRecoverSingleFlightPerCorrelation(t *testing.T) {
	w := newWorld()
	w.crashExec = true
	_ = w.firstProcess(t)
	w.crashExec = false
	w.verifyGate, w.verifyEntered = make(chan struct{}), make(chan struct{})
	a := w.activity(t, recovery.NoDispatch{})
	done := make(chan recovery.Report, 1)
	go func() {
		rep, _ := a.Recover(tctx(), tenantA)
		done <- rep
	}()
	select {
	case <-w.verifyEntered: // the first pass is inside Verify
	case rep := <-done:
		t.Fatalf("the first pass ended without verifying: %+v", rep)
	}
	rep2, err := a.Recover(tctx(), tenantA)
	if err != nil {
		t.Fatal(err)
	}
	if got := outcomeOf(rep2, invID); got != recovery.Skipped {
		t.Fatalf("the second pass gave %q, want %q", got, recovery.Skipped)
	}
	close(w.verifyGate)
	if got := outcomeOf(<-done, invID); got != recovery.Resumed {
		t.Fatalf("the first pass gave %q", got)
	}
	if _, _, verify := w.counts(); verify != 1 {
		t.Fatalf("verified %d times, want once", verify)
	}
}

// The startup pass of G1 runs every tenant it is given, each scoped to its own.
func TestStartupPassRunsEachTenant(t *testing.T) {
	w := newWorld()
	w.crashExec = true
	_ = w.firstProcess(t)
	w.crashExec = false
	reps, err := w.activity(t, recovery.NoDispatch{}).StartupPass(tctx(), []string{tenantB, tenantA})
	if err != nil {
		t.Fatal(err)
	}
	if len(reps) != 2 || outcomeOf(reps[tenantA], invID) != recovery.Resumed || len(reps[tenantB].Items) != 0 {
		t.Fatalf("reports %+v", reps)
	}
}

// A330, AGENT-16: the idle age and the batch are settings with no default in code; a missing or malformed value builds nothing and the error never echoes the value.
func TestLoadConfigHasNoDefault(t *testing.T) {
	get := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	for name, m := range map[string]map[string]string{
		"nothing set":      {},
		"no idle":          {recovery.EnvBatchSize: "50", recovery.EnvRunTimeoutMS: "1000"},
		"no batch":         {recovery.EnvIdleMS: "60000", recovery.EnvRunTimeoutMS: "1000"},
		"zero idle":        {recovery.EnvIdleMS: "0", recovery.EnvBatchSize: "50", recovery.EnvRunTimeoutMS: "1000"},
		"leading zero":     {recovery.EnvIdleMS: "060000", recovery.EnvBatchSize: "50", recovery.EnvRunTimeoutMS: "1000"},
		"signed":           {recovery.EnvIdleMS: "+5", recovery.EnvBatchSize: "50", recovery.EnvRunTimeoutMS: "1000"},
		"space":            {recovery.EnvIdleMS: "5 ", recovery.EnvBatchSize: "50", recovery.EnvRunTimeoutMS: "1000"},
		"batch not number": {recovery.EnvIdleMS: "60000", recovery.EnvBatchSize: "lots-SECRET", recovery.EnvRunTimeoutMS: "1000"},
		"overflow":         {recovery.EnvIdleMS: "99999999999999999999999", recovery.EnvBatchSize: "50", recovery.EnvRunTimeoutMS: "1000"},
		"batch too large":  {recovery.EnvIdleMS: "60000", recovery.EnvBatchSize: "4294967296"},
		"idle too large":   {recovery.EnvIdleMS: "9223372036854775807", recovery.EnvBatchSize: "50", recovery.EnvRunTimeoutMS: "1000"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := recovery.LoadConfig(get(m))
			if err == nil {
				t.Fatal("expected an error")
			}
			if strings.Contains(err.Error(), "SECRET") {
				t.Fatal("the error echoes the value")
			}
		})
	}
	c, err := recovery.LoadConfig(get(map[string]string{recovery.EnvIdleMS: "60000", recovery.EnvBatchSize: "50", recovery.EnvRunTimeoutMS: "1000"}))
	if err != nil || c.IdleMS != 60000 || c.Batch != 50 || c.RunTimeoutMS != 1000 {
		t.Fatalf("got %+v, %v", c, err)
	}
	if recovery.EnvIdleMS != "NOEMA_STM_RECOVERY_IDLE_MS" || recovery.EnvBatchSize != "NOEMA_STM_RECOVERY_BATCH_SIZE" || recovery.EnvRunTimeoutMS != "NOEMA_INVESTIGATION_RUN_TIMEOUT_MS" {
		t.Fatal("the setting names are part of the contract with deployment")
	}
}

// A477 (interim, relayed, not yet committed), AGENT-16: an idle age that does not exceed the run timeout would take up a live run a second time. The loader and the
// constructor refuse it, and a missing run timeout; the errors name the settings and never echo a value.
func TestIdleAgeMustExceedRunTimeout(t *testing.T) {
	get := func(idle, run string) func(string) string {
		return func(k string) string {
			switch k {
			case recovery.EnvIdleMS:
				return idle
			case recovery.EnvBatchSize:
				return "50"
			case recovery.EnvRunTimeoutMS:
				return run
			}
			return ""
		}
	}
	for name, c := range map[string][2]string{"equal": {"240000", "240000"}, "below": {"1000", "240000"}, "no run timeout": {"240000", ""}, "bad run timeout": {"240000", "0777-SECRET"}} {
		_, err := recovery.LoadConfig(get(c[0], c[1]))
		if err == nil {
			t.Fatalf("%s: expected a refusal", name)
		}
		if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "240000") {
			t.Fatalf("%s: the error echoes a value: %v", name, err)
		}
		if name != "no run timeout" && name != "bad run timeout" && (!strings.Contains(err.Error(), recovery.EnvIdleMS) || !strings.Contains(err.Error(), recovery.EnvRunTimeoutMS)) {
			t.Fatalf("%s: the error must name both settings: %v", name, err)
		}
	}
	if c, err := recovery.LoadConfig(get("240001", "240000")); err != nil || c.RunTimeoutMS != 240000 {
		t.Fatalf("got %+v, %v", c, err)
	}
	w := newWorld()
	r := w.runner(t, recovery.NoDispatch{})
	for name, cfg := range map[string]recovery.Config{
		"equal":  {IdleMS: 5, Batch: 1, RunTimeoutMS: 5},
		"below":  {IdleMS: 4, Batch: 1, RunTimeoutMS: 5},
		"no run": {IdleMS: 5, Batch: 1},
	} {
		if _, err := recovery.New(cfg, w, w, w, w, r); err == nil {
			t.Errorf("%s: New must refuse", name)
		}
	}
}

func TestNewRefusesAMissingPart(t *testing.T) {
	w := newWorld()
	r := w.runner(t, recovery.NoDispatch{})
	ok := recovery.Config{IdleMS: 2, Batch: 1, RunTimeoutMS: 1}
	for name, f := range map[string]func() (*recovery.Activity, error){
		"no reader":   func() (*recovery.Activity, error) { return recovery.New(ok, nil, w, w, w, r) },
		"no sessions": func() (*recovery.Activity, error) { return recovery.New(ok, w, nil, w, w, r) },
		"no journal":  func() (*recovery.Activity, error) { return recovery.New(ok, w, w, nil, w, r) },
		"no admit":    func() (*recovery.Activity, error) { return recovery.New(ok, w, w, w, nil, r) },
		"no resumer":  func() (*recovery.Activity, error) { return recovery.New(ok, w, w, w, w, nil) },
		"zero config": func() (*recovery.Activity, error) { return recovery.New(recovery.Config{}, w, w, w, w, r) },
	} {
		if _, err := f(); err == nil {
			t.Errorf("%s: expected a refusal", name)
		}
	}
}

func TestNoDispatchRefuses(t *testing.T) {
	if _, err := (recovery.NoDispatch{}).Execute(tctx(), orchestrator.Session{}, grantID); !errors.Is(err, recovery.ErrNoDispatch) {
		t.Fatalf("got %v", err)
	}
}

// INV-12, AGENT-10, A149, A490 (interim, relayed): the one recovery constructor builds the session from the stored row alone. Tenant and principal are the row's, role and
// authn reference are the stored principal_role and authn_ref, the principal type is the edge's single mapping of that role, and AssertedBy stays empty (the verified issuer is not stored).
func TestRecoverySessionComesOnlyFromTheRow(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ten, prn := rapid.StringMatching(`ten_[0-9A-Za-z]{2,32}`).Draw(t, "tenant"), rapid.StringMatching(`prn_[0-9A-Za-z]{2,32}`).Draw(t, "principal")
		role := rapid.SampledFrom([]string{"customer", "ops", "approver", "audit"}).Draw(t, "role")
		ref := rapid.StringMatching(`ses_[0-9a-f]{32}`).Draw(t, "authn_ref")
		s, err := recovery.NewRecoverySession(recovery.Candidate{TenantID: ten, PrincipalRef: prn}, investigations.RecordedSession{PrincipalID: prn, PrincipalRole: role, AuthnRef: ref})
		if err != nil {
			t.Fatal(err)
		}
		pt, _ := session.PrincipalTypeOf(role)
		if want := (orchestrator.Session{TenantID: ten, PrincipalID: prn, Role: role, AuthnRef: ref, PrincipalType: pt}); s != want {
			t.Fatalf("session %+v, want %+v", s, want)
		}
		if s.AssertedBy != "" {
			t.Fatal("AssertedBy was invented: the verified issuer is not stored")
		}
	})
	good := investigations.RecordedSession{PrincipalID: principal, PrincipalRole: "customer", AuthnRef: authnRef}
	ok := recovery.Candidate{TenantID: tenantA, PrincipalRef: principal}
	for name, c := range map[string]struct {
		c  recovery.Candidate
		rs investigations.RecordedSession
	}{
		"empty":               {recovery.Candidate{}, investigations.RecordedSession{}},
		"no tenant":           {recovery.Candidate{PrincipalRef: principal}, good},
		"no principal":        {recovery.Candidate{TenantID: tenantA}, good},
		"newline tenant":      {recovery.Candidate{TenantID: "ten_A\nB", PrincipalRef: principal}, good},
		"blank in principal":  {recovery.Candidate{TenantID: tenantA, PrincipalRef: "prn A"}, good},
		"no role":             {ok, investigations.RecordedSession{PrincipalID: principal, AuthnRef: authnRef}},
		"no authn ref":        {ok, investigations.RecordedSession{PrincipalID: principal, PrincipalRole: "customer"}},
		"role outside A1":     {ok, investigations.RecordedSession{PrincipalID: principal, PrincipalRole: "admin", AuthnRef: authnRef}},
		"role in other case":  {ok, investigations.RecordedSession{PrincipalID: principal, PrincipalRole: "Customer", AuthnRef: authnRef}},
		"authn ref malformed": {ok, investigations.RecordedSession{PrincipalID: principal, PrincipalRole: "customer", AuthnRef: "ses_zz"}},
		"principal differs":   {ok, investigations.RecordedSession{PrincipalID: "prn_OTHER", PrincipalRole: "customer", AuthnRef: authnRef}},
	} {
		if _, err := recovery.NewRecoverySession(c.c, c.rs); err == nil {
			t.Errorf("%s: expected a refusal", name)
		}
	}
}

// A490 (interim, relayed), INV-12, AGENT-10: a row with no recorded session (a legacy row, from before migration 0036) is never resumed and never given an invented role. The
// sweep names it with a fixed outcome and counts it; no port is called for it, and the other rows are still resumed.
func TestRecoverNeverResumesALegacyRowAndCountsIt(t *testing.T) {
	w := newWorld()
	dec := orchestrator.Decision{ID: decisionID, Outcome: contracts.EnumsV1DecisionOutcomeALLOW, GrantIDs: []string{grantID}, IntentRef: intentRef}
	w.rows[invID].inv = orchestrator.Investigation{ID: invID, State: "AWAITING_VERIFICATION", Version: 7}
	w.rows[invID].decision = dec
	w.rows[invID].role, w.rows[invID].authn = "", "" // a legacy row: both columns are NULL
	w.rows["inv_BB22"] = &row{tenant: tenantA, principal: principal, role: "customer", authn: authnRef, corr: "corr_BB22", recev: "recev_BB22",
		inv: orchestrator.Investigation{ID: "inv_BB22", State: "AWAITING_VERIFICATION", Version: 7}, decision: dec}
	rep, err := w.activity(t, recovery.NoDispatch{}).Recover(tctx(), tenantA)
	if err != nil {
		t.Fatal(err)
	}
	if got := outcomeOf(rep, invID); got != recovery.NoRecordedSession || string(got) != "NO_RECORDED_SESSION" {
		t.Fatalf("legacy row: outcome %q, want NO_RECORDED_SESSION", got)
	}
	if got := outcomeOf(rep, "inv_BB22"); got != recovery.Resumed {
		t.Fatalf("the row with a session: outcome %q, want %q", got, recovery.Resumed)
	}
	if rep.NoRecordedSession != 1 {
		t.Fatalf("the sweep counted %d rows without a session, want 1", rep.NoRecordedSession)
	}
	if w.state() != "AWAITING_VERIFICATION" {
		t.Fatalf("the legacy row moved to %s", w.state())
	}
	for port, ss := range w.sessions {
		for _, s := range ss {
			if s.Role == "" || s.AuthnRef == "" {
				t.Fatalf("port %s saw a session with no role or authn reference: %+v", port, s)
			}
		}
	}
}

// A490, A149: a row resumes under its stored role and no other. A customer row is a customer, an approver row an approver; the principal type is the edge's mapping; every port of
// the resume path sees exactly that session. Property: whatever the stored role, no value from anywhere else reaches the session.
func TestRecoverResumesUnderTheStoredRole(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		role := rapid.SampledFrom([]string{"customer", "ops", "approver", "audit"}).Draw(t, "role")
		ref := rapid.StringMatching(`ses_[0-9a-f]{32}`).Draw(t, "authn_ref")
		w := newWorld()
		w.rows[invID].inv = orchestrator.Investigation{ID: invID, State: "AWAITING_VERIFICATION", Version: 7}
		w.rows[invID].decision = orchestrator.Decision{ID: decisionID, Outcome: contracts.EnumsV1DecisionOutcomeALLOW, GrantIDs: []string{grantID}, IntentRef: intentRef}
		w.rows[invID].role, w.rows[invID].authn = role, ref
		rep, err := w.activity2(t).Recover(tctx(), tenantA)
		if err != nil {
			t.Fatal(err)
		}
		if outcomeOf(rep, invID) != recovery.Resumed {
			t.Fatalf("report %+v", rep)
		}
		pt, _ := session.PrincipalTypeOf(role)
		want := orchestrator.Session{TenantID: tenantA, PrincipalID: principal, Role: role, AuthnRef: ref, PrincipalType: pt}
		if len(w.sessions) == 0 {
			t.Fatal("no port was reached")
		}
		for port, ss := range w.sessions {
			for _, s := range ss {
				if s != want {
					t.Fatalf("port %s saw session %+v, want %+v", port, s, want)
				}
			}
		}
	})
}

// A490, A1: a stored role outside the A1 set, or an authn reference that is not a session reference, is refused and counted; no port is called and nothing is invented.
func TestRecoverRefusesAStoredSessionOutsideItsSet(t *testing.T) {
	for name, rs := range map[string][2]string{"unknown role": {"admin", authnRef}, "upper-case role": {"Customer", authnRef}, "malformed authn ref": {"customer", "not-a-ref"}} {
		t.Run(name, func(t *testing.T) {
			w := newWorld()
			w.rows[invID].inv = orchestrator.Investigation{ID: invID, State: "AWAITING_VERIFICATION", Version: 7}
			w.rows[invID].role, w.rows[invID].authn = rs[0], rs[1]
			rep, err := w.activity(t, recovery.NoDispatch{}).Recover(tctx(), tenantA)
			if err != nil {
				t.Fatal(err)
			}
			if got := outcomeOf(rep, invID); got != recovery.Refused || rep.Refused != 1 || rep.NoRecordedSession != 0 {
				t.Fatalf("outcome %q, refused %d, no-session %d", got, rep.Refused, rep.NoRecordedSession)
			}
			if len(w.sessions) != 0 || w.state() != "AWAITING_VERIFICATION" {
				t.Fatalf("a port was called or the row moved (%s) for a refused row", w.state())
			}
		})
	}
}

// REQ-SEC-004, INV-12: the stored session is read for the pass's tenant. Another tenant's row is refused before the store is asked, so it is not resumed and not counted as a
// legacy row.
func TestRecoverStoredSessionIsTenantScoped(t *testing.T) {
	w := newWorld()
	w.rows[invID].inv = orchestrator.Investigation{ID: invID, State: "AWAITING_VERIFICATION", Version: 7}
	w.rows[invID].tenant = tenantB
	w.rows[invID].role, w.rows[invID].authn = "", "" // would be counted if the pass looked
	w.batchIgnored = true                            // the reader hands back tenant B's row when asked for A
	rep, err := w.activity(t, recovery.NoDispatch{}).Recover(tctx(), tenantA)
	if err != nil {
		t.Fatal(err)
	}
	if got := outcomeOf(rep, invID); got != recovery.Refused {
		t.Fatalf("outcome %q, want %q", got, recovery.Refused)
	}
	if rep.NoRecordedSession != 0 || len(w.sessions) != 0 {
		t.Fatalf("another tenant's row was counted or reached a port: %+v", rep)
	}
	for _, tn := range w.sessionTenants {
		if tn != tenantA {
			t.Fatalf("the stored session was read for tenant %s", tn)
		}
	}
}

// A490: a store that cannot answer is Failed (the next pass tries again), never an invented session; a row the store does not find is Refused.
func TestRecoverSessionReadFailureIsFailed(t *testing.T) {
	w := newWorld()
	w.rows[invID].inv = orchestrator.Investigation{ID: invID, State: "AWAITING_VERIFICATION", Version: 7}
	w.sessionErr = errors.New("the database is down")
	rep, err := w.activity(t, recovery.NoDispatch{}).Recover(tctx(), tenantA)
	if err != nil {
		t.Fatal(err)
	}
	if got := outcomeOf(rep, invID); got != recovery.Failed || len(w.sessions) != 0 {
		t.Fatalf("outcome %q, ports reached %d", got, len(w.sessions))
	}
	w.sessionErr = investigations.ErrNoInvestigation
	rep, err = w.activity(t, recovery.NoDispatch{}).Recover(tctx(), tenantA)
	if err != nil {
		t.Fatal(err)
	}
	if got := outcomeOf(rep, invID); got != recovery.Refused {
		t.Fatalf("a row the store does not find: outcome %q, want %q", got, recovery.Refused)
	}
}

// A490: the startup pass carries each tenant's count, and the total over tenants is what the caller logs and alerts on.
func TestStartupPassAggregatesTheNoSessionCount(t *testing.T) {
	w := newWorld()
	w.rows[invID].role, w.rows[invID].authn = "", ""
	w.rows["inv_BB22"] = &row{tenant: tenantB, principal: "prn_BB22", role: "customer", authn: authnRef, corr: "corr_BB22", recev: "recev_BB22", inv: orchestrator.Investigation{ID: "inv_BB22", State: "RECEIVED"}}
	w.rows["inv_CC33"] = &row{tenant: tenantB, principal: "prn_BB22", corr: "corr_CC33", recev: "recev_CC33", inv: orchestrator.Investigation{ID: "inv_CC33", State: "RECEIVED"}}
	w.rows["inv_DD44"] = &row{tenant: tenantB, principal: "prn_BB22", corr: "corr_DD44", recev: "recev_DD44", inv: orchestrator.Investigation{ID: "inv_DD44", State: "RECEIVED"}}
	out, err := w.activity(t, recovery.NoDispatch{}).StartupPass(tctx(), []string{tenantA, tenantB})
	if err != nil {
		t.Fatal(err)
	}
	if out[tenantA].NoRecordedSession != 1 || out[tenantB].NoRecordedSession != 2 {
		t.Fatalf("per-tenant counts %d and %d, want 1 and 2", out[tenantA].NoRecordedSession, out[tenantB].NoRecordedSession)
	}
	if got := recovery.TotalNoRecordedSession(out); got != 3 {
		t.Fatalf("total %d, want 3", got)
	}
}

// Nothing request-derived reaches either path (A377 item 15): for any stored row, every port of the resume path (admissions, bindings, journal, verifier, recorder) sees the
// session built from the row and no other, and the turn is made of stored values and constants only. The activity takes a tenant and nothing else; there is no request.
func TestNoRequestDerivedValueReachesEitherPath(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		w := newWorld()
		w.rows[invID].inv = orchestrator.Investigation{ID: invID, State: "AWAITING_VERIFICATION", Version: 7}
		w.rows[invID].principal = rapid.StringMatching(`prn_[0-9A-Za-z]{2,32}`).Draw(t, "principal")
		w.rows[invID].decision = orchestrator.Decision{ID: decisionID, Outcome: contracts.EnumsV1DecisionOutcomeALLOW, GrantIDs: []string{grantID}, IntentRef: intentRef}
		rep, err := w.activity2(t).Recover(tctx(), tenantA)
		if err != nil {
			t.Fatal(err)
		}
		if outcomeOf(rep, invID) != recovery.Resumed {
			t.Fatalf("report %+v", rep)
		}
		want := orchestrator.Session{TenantID: tenantA, PrincipalID: w.rows[invID].principal, Role: "customer", AuthnRef: authnRef, PrincipalType: "customer"}
		for port, ss := range w.sessions {
			for _, s := range ss {
				if s != want {
					t.Fatalf("port %s saw session %+v, want %+v", port, s, want)
				}
			}
		}
		for _, port := range []string{"admissions", "bindings", "journal", "verifier", "recorder"} {
			if len(w.sessions[port]) == 0 && port != "journal" {
				t.Fatalf("port %s was not reached", port)
			}
		}
	})
}

func (w *world) activity2(t *rapid.T) *recovery.Activity {
	tab, err := orchestrator.LoadTable()
	if err != nil {
		t.Fatal(err)
	}
	r, err := orchestrator.NewWriteRunner(&orchestrator.Machine{Table: tab},
		orchestrator.Ports{Interpreter: w, Evidence: w, Trust: w, Explainer: w, Recorder: w, Admissions: w},
		orchestrator.WritePorts{Executor: recovery.NoDispatch{}, Verifier: w, Journal: w, Bindings: w}, orchestrator.WriteConfig{MaxDispatchesPerKey: orchestrator.PERDMaxDispatchesPerKey})
	if err != nil {
		t.Fatal(err)
	}
	a, err := recovery.New(recovery.Config{IdleMS: 2, Batch: 1, RunTimeoutMS: 1}, w, w, w, w, r)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// Another process moved the row after the list was read: the compare-and-swap refuses and the investigation is skipped, never forced.
func TestRecoverSkipsARowAnotherProcessMoved(t *testing.T) {
	w := newWorld()
	w.crashVerify = true
	_ = w.firstProcess(t)
	w.crashVerify, w.staleRead = false, true
	rep, err := w.activity(t, recovery.NoDispatch{}).Recover(tctx(), tenantA)
	if err != nil {
		t.Fatal(err)
	}
	if got := outcomeOf(rep, invID); got != recovery.Skipped {
		t.Fatalf("outcome %q, want %q", got, recovery.Skipped)
	}
}

// Every call to a store or port in this repository carries a deadline; a pass without one is refused before anything is read.
func TestRecoverNeedsADeadline(t *testing.T) {
	w := newWorld()
	if _, err := w.activity(t, recovery.NoDispatch{}).Recover(context.Background(), tenantA); err == nil || len(w.readerCalls) != 0 {
		t.Fatalf("a pass without a deadline ran: %v", err)
	}
}
