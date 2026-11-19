package trustport_test

import (
	"context"
	"errors"
	"math/rand"
	"testing"
	"time"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/trust/confirm"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// TRU-12, the full confirmation path (founder decision of 19 Nov 2026, interim, relayed, not yet committed). Proves REQ-TRU-009, REQ-AMB-004; INV-12, INV-17, INV-25.
// The store here is an in-memory stand-in for the unit logic; the Postgres store is tested against a real database in trustpg2. The pack fixture is a test-only
// profile (no industry terms in core). All data is SYNTHETIC (AGENT-25).

var cpNow = time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

const cpTTL = time.Hour

type cpPolicy struct {
	terms trustport.ConfirmationTerms
	err   error
}

func (p *cpPolicy) Terms(context.Context, intake.AdmittedIntent) (trustport.ConfirmationTerms, error) {
	return p.terms, p.err
}

type cpPlanner struct {
	plan  intake.Plan
	err   error
	calls []intake.PlanRequest
}

func (p *cpPlanner) Plan(_ context.Context, r intake.PlanRequest) (intake.Plan, error) {
	p.calls = append(p.calls, r)
	return p.plan, p.err
}

type cpFresh struct {
	v   trustport.FreshValues
	err error
}

func (f *cpFresh) Fresh(context.Context, intake.AdmittedIntent, intake.Plan) (trustport.FreshValues, error) {
	return f.v, f.err
}

type cpStore struct {
	req     trustport.StoredRequest
	found   bool
	opened  []trustport.NewConfirmationRequest
	expired []string
	err     error
	seq     int
}

func (s *cpStore) Latest(context.Context, string, string, string, int) (trustport.StoredRequest, bool, error) {
	return s.req, s.found, s.err
}

func (s *cpStore) Open(_ context.Context, r trustport.NewConfirmationRequest) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	s.opened = append(s.opened, r)
	s.seq++
	return "cfr_new" + string(rune('A'+s.seq)), nil
}

func (s *cpStore) Expire(_ context.Context, _, id string) error {
	s.expired = append(s.expired, id)
	return s.err
}

type cpResolutions struct {
	got []trustport.ResolvedConfirmation
	err error
}

func (r *cpResolutions) Record(_ context.Context, c trustport.ResolvedConfirmation) error {
	r.got = append(r.got, c)
	return r.err
}

type cpWorld struct {
	pol  *cpPolicy
	plan *cpPlanner
	fr   *cpFresh
	st   *cpStore
	res  *cpResolutions
	path trustport.ConfirmationPath
}

func cpAdmitted() intake.AdmittedIntent {
	return intake.AdmittedIntent{TenantID: "ten_1", IntentID: "int_1", Version: 1, IntentNode: "node_1", IntentHash: "sha256:aa", PrincipalID: "prn_1", CorrelationID: "cor_1", TargetRef: "acct_1"}
}

func cpPlan() intake.Plan {
	return intake.Plan{Hash: "sha256:dd", Steps: []intake.PlanStep{{CapabilityID: "cap_1", TargetRef: "acct_1", ParamsHash: "sha256:bb"}}}
}

func cpBinding() confirm.BindingInput {
	return confirm.BindingInput{PrincipalID: "prn_1", IntentHash: "sha256:aa", CapabilityID: "cap_1", TargetRef: "acct_1", ParamsHash: "sha256:bb",
		EvidenceSetHash: "sha256:cc", PlanHash: "sha256:dd", PolicyVersion: "pol_1"}
}

func cpOpen(requirement string) trustport.StoredRequest {
	return trustport.StoredRequest{Request: confirm.Request{ID: "cfr_1", Binding: cpBinding(), Requirement: requirement, RequesterID: "prn_1", ExpiresAt: cpNow.Add(cpTTL).Unix()}}
}

func cpAnswered(requirement string, ev confirm.Event) trustport.StoredRequest {
	r := cpOpen(requirement)
	r.Answer = &trustport.StoredAnswer{EventID: "cfe_1", Event: ev}
	return r
}

func cpAccept(by string, roles ...string) confirm.Event {
	return confirm.Event{RequestID: "cfr_1", ApproverID: by, ApproverRoles: roles, Response: confirm.Accept, At: cpNow.Add(-time.Minute).Unix()}
}

func cpBuild(t *testing.T, requirement string, st *cpStore) *cpWorld {
	t.Helper()
	w := &cpWorld{
		pol:  &cpPolicy{terms: trustport.ConfirmationTerms{Required: true, Requirement: requirement, ExpiresIn: cpTTL}},
		plan: &cpPlanner{plan: cpPlan()},
		fr:   &cpFresh{v: trustport.FreshValues{EvidenceSetHash: "sha256:cc", PolicyVersion: "pol_1"}},
		st:   st, res: &cpResolutions{},
	}
	p, err := trustport.NewConfirmationPath(trustport.ConfirmationDeps{Policy: w.pol, Planner: w.plan, Fresh: w.fr, Store: w.st, Resolutions: w.res, Clock: clock.NewManual(cpNow)})
	if err != nil {
		t.Fatal(err)
	}
	w.path = p
	return w
}

func cpCtx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

func TestPathNotRequiredWhenThePolicyAsksForNone(t *testing.T) {
	w := cpBuild(t, "SELF", &cpStore{err: errors.New("must not be read")})
	w.pol.terms.Required = false
	got, err := w.path.Evaluate(cpCtx(t), cpAdmitted())
	if err != nil || got.State != trustport.ConfirmationStateNotRequired || len(w.plan.calls) != 0 {
		t.Fatalf("got %+v %v planner calls %d", got, err, len(w.plan.calls))
	}
	st, err := w.path.Status(cpCtx(t), cpAdmitted())
	if err != nil || st.Status != intake.ConfirmationNotRequired {
		t.Fatalf("status %+v %v", st, err)
	}
}

func TestPathNoRequestYetOpensOneWithTheEightBindingValues(t *testing.T) {
	w := cpBuild(t, "MAKER_CHECKER", &cpStore{})
	got, err := w.path.Evaluate(cpCtx(t), cpAdmitted())
	if err != nil || got.State != trustport.ConfirmationStatePending || got.RequestID != "cfr_newB" {
		t.Fatalf("got %+v %v", got, err)
	}
	if len(w.st.opened) != 1 {
		t.Fatalf("%d requests opened, want 1", len(w.st.opened))
	}
	o := w.st.opened[0]
	if o.Binding != cpBinding() || o.Requirement != "MAKER_CHECKER" || o.RequesterID != "prn_1" || o.ExpiresAt != cpNow.Add(cpTTL).Unix() || o.TenantID != "ten_1" ||
		o.CorrelationID != "cor_1" || o.Supersedes != "" {
		t.Fatalf("opened %+v", o)
	}
	if len(w.plan.calls) != 1 || !w.plan.calls[0].WriteRequired || w.plan.calls[0].IntentHash != "sha256:aa" || w.plan.calls[0].TenantID != "ten_1" {
		t.Fatalf("planner asked %+v", w.plan.calls)
	}
	st, err := w.path.Status(cpCtx(t), cpAdmitted())
	if err != nil || st.Status != intake.ConfirmationPending || st.EventID != "" {
		t.Fatalf("status %+v %v", st, err)
	}
}

func TestPathAnOpenUnexpiredRequestWithNoAnswerIsPendingWithItsIDAndNeverASecondRequest(t *testing.T) {
	w := cpBuild(t, "SELF", &cpStore{req: cpOpen("SELF"), found: true})
	for i := 0; i < 3; i++ {
		got, err := w.path.Evaluate(cpCtx(t), cpAdmitted())
		if err != nil || got.State != trustport.ConfirmationStatePending || got.RequestID != "cfr_1" {
			t.Fatalf("round %d: %+v %v", i, got, err)
		}
	}
	if len(w.st.opened) != 0 || len(w.st.expired) != 0 || len(w.res.got) != 0 {
		t.Fatalf("opened %d expired %d resolutions %d, want none", len(w.st.opened), len(w.st.expired), len(w.res.got))
	}
}

func TestPathAcceptThatResolvesToProceedIsGivenAndRecordsTheResolution(t *testing.T) {
	w := cpBuild(t, "SELF", &cpStore{req: cpAnswered("SELF", cpAccept("prn_1")), found: true})
	got, err := w.path.Evaluate(cpCtx(t), cpAdmitted())
	if err != nil || got.State != trustport.ConfirmationStateGiven || got.EventID != "cfe_1" || got.PlanHash != "sha256:dd" {
		t.Fatalf("got %+v %v", got, err)
	}
	want, _ := confirm.BindingHash(cpBinding())
	if len(w.res.got) != 1 {
		t.Fatalf("%d resolutions", len(w.res.got))
	}
	r := w.res.got[0]
	if r.TenantID != "ten_1" || r.EventID != "cfe_1" || r.RequestID != "cfr_1" || r.BindingHash != want || !r.ExpiresAt.Equal(cpNow.Add(cpTTL)) || !r.AcceptedAt.Equal(cpNow.Add(-time.Minute)) {
		t.Fatalf("resolution %+v", r)
	}
	st, err := w.path.Status(cpCtx(t), cpAdmitted())
	if err != nil || st.Status != intake.ConfirmationGiven || st.EventID != "cfe_1" || st.PlanHash != "sha256:dd" {
		t.Fatalf("status %+v %v", st, err)
	}
}

func TestPathDoubleAcceptIsIdempotentAndNeverOpensARequest(t *testing.T) {
	w := cpBuild(t, "SELF", &cpStore{req: cpAnswered("SELF", cpAccept("prn_1")), found: true})
	a, errA := w.path.Evaluate(cpCtx(t), cpAdmitted())
	b, errB := w.path.Evaluate(cpCtx(t), cpAdmitted())
	if errA != nil || errB != nil || a != b || a.State != trustport.ConfirmationStateGiven || len(w.st.opened) != 0 {
		t.Fatalf("%+v %v / %+v %v opened %d", a, errA, b, errB, len(w.st.opened))
	}
	if len(w.res.got) != 2 || w.res.got[0] != w.res.got[1] {
		t.Fatalf("the two calls must hand the store the same row (the store inserts it once): %+v", w.res.got)
	}
}

func TestPathDeclineIsDeclinedWithNoNewRequest(t *testing.T) {
	ev := cpAccept("prn_1")
	ev.Response = confirm.Decline
	w := cpBuild(t, "SELF", &cpStore{req: cpAnswered("SELF", ev), found: true})
	got, err := w.path.Evaluate(cpCtx(t), cpAdmitted())
	if err != nil || got.State != trustport.ConfirmationStateDeclined || got.EventID != "" || got.PlanHash != "" {
		t.Fatalf("got %+v %v", got, err)
	}
	if len(w.st.opened) != 0 || len(w.res.got) != 0 {
		t.Fatal("a decline opens no request and records no resolution")
	}
	st, err := w.path.Status(cpCtx(t), cpAdmitted())
	if !errors.Is(err, trustport.ErrDeclined) || st.Status != 0 {
		t.Fatalf("status %+v %v: the intake has no Declined status, so Status fails closed", st, err)
	}
}

func TestPathExpiredRecordsTheOldRequestsExpiryAndOpensAFreshOne(t *testing.T) {
	late := cpOpen("SELF")
	late.Request.ExpiresAt = cpNow.Add(-time.Second).Unix()
	answeredLate := cpAnswered("SELF", cpAccept("prn_1"))
	answeredLate.Request.ExpiresAt = cpNow.Add(-time.Second).Unix()
	answeredLate.Answer.Event.At = answeredLate.Request.ExpiresAt + 1
	for name, req := range map[string]trustport.StoredRequest{"no answer, past expiry": late, "answer after expiry": answeredLate} {
		t.Run(name, func(t *testing.T) {
			w := cpBuild(t, "SELF", &cpStore{req: req, found: true})
			got, err := w.path.Evaluate(cpCtx(t), cpAdmitted())
			if err != nil || got.State != trustport.ConfirmationStatePending || got.RequestID != "cfr_newB" || got.Expired != "cfr_1" || got.Superseded != "cfr_1" {
				t.Fatalf("got %+v %v", got, err)
			}
			if len(w.st.expired) != 1 || w.st.expired[0] != "cfr_1" || len(w.st.opened) != 1 || w.st.opened[0].Supersedes != "cfr_1" || len(w.res.got) != 0 {
				t.Fatalf("expired %v opened %+v resolutions %d", w.st.expired, w.st.opened, len(w.res.got))
			}
		})
	}
}

func TestPathAnAcceptGivenJustBeforeExpiryButUsedAfterItIsExpiredNotGiven(t *testing.T) {
	req := cpAnswered("SELF", cpAccept("prn_1"))
	req.Request.ExpiresAt = cpNow.Add(-time.Second).Unix()
	req.Answer.Event.At = req.Request.ExpiresAt - 5
	w := cpBuild(t, "SELF", &cpStore{req: req, found: true})
	got, err := w.path.Evaluate(cpCtx(t), cpAdmitted())
	if err != nil || got.State != trustport.ConfirmationStatePending || len(w.res.got) != 0 || len(w.st.expired) != 1 {
		t.Fatalf("got %+v %v resolutions %d", got, err, len(w.res.got))
	}
}

func TestPathBindingChangedAndPolicyChangedSupersedeWithAFreshRequestAndRecordNoNewEventType(t *testing.T) {
	cases := map[string]func(*cpWorld){
		"params changed (plan step)": func(w *cpWorld) { w.plan.plan.Steps[0].ParamsHash = "sha256:ee" },
		"plan changed":               func(w *cpWorld) { w.plan.plan.Hash = "sha256:99" },
		"target changed":             func(w *cpWorld) { w.plan.plan.Steps[0].TargetRef = "acct_9" },
		"evidence changed":           func(w *cpWorld) { w.fr.v.EvidenceSetHash = "sha256:ff" },
		"policy changed":             func(w *cpWorld) { w.fr.v.PolicyVersion = "pol_2" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			w := cpBuild(t, "SELF", &cpStore{req: cpAnswered("SELF", cpAccept("prn_1")), found: true})
			mutate(w)
			a := cpAdmitted()
			if name == "target changed" {
				a.TargetRef = "acct_9"
			}
			got, err := w.path.Evaluate(cpCtx(t), a)
			if err != nil || got.State != trustport.ConfirmationStatePending || got.RequestID != "cfr_newB" || got.Superseded != "cfr_1" || got.Expired != "" {
				t.Fatalf("got %+v %v", got, err)
			}
			if len(w.st.opened) != 1 || w.st.opened[0].Supersedes != "cfr_1" || len(w.st.expired) != 0 || len(w.res.got) != 0 {
				t.Fatalf("opened %+v expired %v resolutions %d", w.st.opened, w.st.expired, len(w.res.got))
			}
		})
	}
}

func TestPathNotEligibleAndWrongRequestFailClosedAndOpenNothing(t *testing.T) {
	other := cpAccept("prn_1")
	other.RequestID = "cfr_9"
	maybe := cpAccept("prn_1")
	maybe.Response = "MAYBE"
	cases := map[string]struct {
		req  trustport.StoredRequest
		want error
	}{
		"self, another approver":       {cpAnswered("SELF", cpAccept("prn_9")), trustport.ErrApproverNotEligible},
		"maker-checker, the requester": {cpAnswered("MAKER_CHECKER", cpAccept("prn_1")), trustport.ErrApproverNotEligible},
		"role missing":                 {cpAnswered("ROLE:checker", cpAccept("prn_9", "viewer")), trustport.ErrApproverNotEligible},
		"another request":              {cpAnswered("SELF", other), trustport.ErrWrongRequest},
		"unknown reply":                {cpAnswered("SELF", maybe), trustport.ErrWrongRequest},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := cpBuild(t, "SELF", &cpStore{req: c.req, found: true})
			got, err := w.path.Evaluate(cpCtx(t), cpAdmitted())
			if !errors.Is(err, c.want) || got.State != 0 || len(w.st.opened) != 0 || len(w.res.got) != 0 {
				t.Fatalf("got %+v %v", got, err)
			}
			st, err := w.path.Status(cpCtx(t), cpAdmitted())
			if !errors.Is(err, c.want) || st.Status != 0 {
				t.Fatalf("status %+v %v", st, err)
			}
		})
	}
}

func TestPathEligibilityBySelfMakerCheckerAndRole(t *testing.T) {
	cases := []struct {
		requirement string
		approver    string
		roles       []string
		given       bool
	}{
		{"SELF", "prn_1", nil, true}, {"SELF", "prn_2", nil, false},
		{"MAKER_CHECKER", "prn_2", nil, true}, {"MAKER_CHECKER", "prn_1", nil, false},
		{"ROLE:checker", "prn_2", []string{"checker"}, true}, {"ROLE:checker", "prn_2", []string{"other"}, false}, {"ROLE:checker", "prn_1", []string{"checker"}, true},
	}
	for _, c := range cases {
		w := cpBuild(t, c.requirement, &cpStore{req: cpAnswered(c.requirement, cpAccept(c.approver, c.roles...)), found: true})
		got, err := w.path.Evaluate(cpCtx(t), cpAdmitted())
		if c.given && (err != nil || got.State != trustport.ConfirmationStateGiven) {
			t.Fatalf("%+v: got %+v %v", c, got, err)
		}
		if !c.given && !errors.Is(err, trustport.ErrApproverNotEligible) {
			t.Fatalf("%+v: got %+v %v", c, got, err)
		}
	}
}

// INV-25: a grant can follow only a Proceed. Whatever the stored answer and the fresh values, Given (and a resolution row) appear exactly when confirm.Resolve says Proceed,
// and never otherwise. Seeded, so a failure reproduces.
func TestPathPropertyNoGivenFromANonProceed(t *testing.T) {
	rng := rand.New(rand.NewSource(25))
	reqs := []string{"SELF", "MAKER_CHECKER", "ROLE:checker", "ROLE:", "BOGUS"}
	pick := func(xs ...string) string { return xs[rng.Intn(len(xs))] }
	for i := 0; i < 600; i++ {
		req := pick(reqs...)
		stored := cpAnswered(req, confirm.Event{RequestID: pick("cfr_1", "cfr_1", "cfr_2"), ApproverID: pick("prn_1", "prn_2", ""), ApproverRoles: []string{pick("checker", "viewer")},
			Response: confirm.Response(pick("ACCEPT", "ACCEPT", "DECLINE", "MAYBE", "")), At: cpNow.Add(time.Duration(rng.Intn(7200)-3600) * time.Second).Unix()})
		stored.Request.ExpiresAt = cpNow.Add(time.Duration(rng.Intn(7200)-3600) * time.Second).Unix()
		w := cpBuild(t, req, &cpStore{req: stored, found: true})
		w.fr.v = trustport.FreshValues{EvidenceSetHash: pick("sha256:cc", "sha256:c2"), PolicyVersion: pick("pol_1", "pol_2")}
		w.plan.plan.Steps[0].ParamsHash = pick("sha256:bb", "sha256:b2")
		got, err := w.path.Evaluate(cpCtx(t), cpAdmitted())
		fresh := cpBinding()
		fresh.EvidenceSetHash, fresh.PolicyVersion, fresh.ParamsHash = w.fr.v.EvidenceSetHash, w.fr.v.PolicyVersion, w.plan.plan.Steps[0].ParamsHash
		res, rerr := confirm.Resolve(stored.Request, stored.Answer.Event, fresh)
		wantGiven := rerr == nil && res == confirm.Proceed && cpNow.Unix() <= stored.Request.ExpiresAt
		isGiven := err == nil && got.State == trustport.ConfirmationStateGiven
		if isGiven != wantGiven || (isGiven && got.EventID == "") || (len(w.res.got) > 0) != wantGiven {
			t.Fatalf("iteration %d: %+v %+v -> %+v %v; resolve %v %v; resolutions %d", i, stored.Request, stored.Answer.Event, got, err, res, rerr, len(w.res.got))
		}
		if !isGiven && got.State == 0 && err == nil {
			t.Fatalf("iteration %d: a zero state with no error", i)
		}
	}
}

func TestPathEveryDependencyFailureIsAnErrorNeverPendingOrGiven(t *testing.T) {
	boom := errors.New("down")
	cases := map[string]func(*cpWorld){
		"policy":      func(w *cpWorld) { w.pol.err = boom },
		"planner":     func(w *cpWorld) { w.plan.err = boom },
		"fresh":       func(w *cpWorld) { w.fr.err = boom },
		"store":       func(w *cpWorld) { w.st.err = boom },
		"resolutions": func(w *cpWorld) { w.res.err = boom },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			w := cpBuild(t, "SELF", &cpStore{req: cpAnswered("SELF", cpAccept("prn_1")), found: true})
			mutate(w)
			got, err := w.path.Evaluate(cpCtx(t), cpAdmitted())
			if !errors.Is(err, boom) || got.State != 0 || got.EventID != "" {
				t.Fatalf("got %+v %v", got, err)
			}
			if st, err := w.path.Status(cpCtx(t), cpAdmitted()); !errors.Is(err, boom) || st.Status != 0 {
				t.Fatalf("status %+v %v", st, err)
			}
		})
	}
}

func TestPathRefusesABadPolicyAPlanWithNoOrManyStepsAndABadAdmittedIntent(t *testing.T) {
	for name, mutate := range map[string]func(*cpWorld){
		"unknown requirement": func(w *cpWorld) { w.pol.terms.Requirement = "ANYONE" },
		"no expiry":           func(w *cpWorld) { w.pol.terms.ExpiresIn = 0 },
		"no step":             func(w *cpWorld) { w.plan.plan.Steps = nil },
		"two steps":           func(w *cpWorld) { w.plan.plan.Steps = append(w.plan.plan.Steps, w.plan.plan.Steps[0]) },
		"unsafe value":        func(w *cpWorld) { w.fr.v.PolicyVersion = "bad value" },
		"plan invalid":        func(w *cpWorld) { w.plan.err = intake.ErrPlanInvalid },
	} {
		t.Run(name, func(t *testing.T) {
			w := cpBuild(t, "SELF", &cpStore{})
			mutate(w)
			got, err := w.path.Evaluate(cpCtx(t), cpAdmitted())
			if err == nil || got.State != 0 || len(w.st.opened) != 0 {
				t.Fatalf("got %+v %v opened %d", got, err, len(w.st.opened))
			}
		})
	}
	for name, mutate := range map[string]func(*intake.AdmittedIntent){
		"tenant":    func(a *intake.AdmittedIntent) { a.TenantID = "" },
		"principal": func(a *intake.AdmittedIntent) { a.PrincipalID = "" },
		"intent":    func(a *intake.AdmittedIntent) { a.IntentID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			a := cpAdmitted()
			mutate(&a)
			w := cpBuild(t, "SELF", &cpStore{})
			if _, err := w.path.Evaluate(cpCtx(t), a); !errors.Is(err, trustport.ErrSession) {
				t.Fatalf("%v", err)
			}
		})
	}
	if _, err := trustport.NewConfirmationPath(trustport.ConfirmationDeps{}); !errors.Is(err, trustport.ErrConfig) {
		t.Fatalf("no ports: %v", err)
	}
	var zero trustport.ConfirmationPath
	if _, err := zero.Evaluate(cpCtx(t), cpAdmitted()); !errors.Is(err, trustport.ErrConfig) {
		t.Fatalf("zero value: %v", err)
	}
}

func TestPathAStoredRequestOfAnotherPrincipalOrIntentIsSupersededNeverGiven(t *testing.T) {
	for name, mutate := range map[string]func(*trustport.StoredRequest){
		"principal": func(r *trustport.StoredRequest) { r.Request.Binding.PrincipalID = "prn_9" },
		"requester": func(r *trustport.StoredRequest) { r.Request.RequesterID = "prn_9" },
		"intent":    func(r *trustport.StoredRequest) { r.Request.Binding.IntentHash = "sha256:99" },
	} {
		t.Run(name, func(t *testing.T) {
			req := cpAnswered("SELF", cpAccept("prn_1"))
			mutate(&req)
			w := cpBuild(t, "SELF", &cpStore{req: req, found: true})
			got, err := w.path.Evaluate(cpCtx(t), cpAdmitted())
			if err != nil || got.State != trustport.ConfirmationStatePending || len(w.res.got) != 0 || len(w.st.opened) != 1 {
				t.Fatalf("got %+v %v", got, err)
			}
		})
	}
}
