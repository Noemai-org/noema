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

// TRU-12, the confirmation adapter conforming to the merged port of noema#757 (founder order of 19 Nov 2026, A341, interim, relayed, not yet committed). Proves REQ-TRU-009,
// REQ-AMB-004; INV-12, INV-17, INV-25. Status is PURE: the store has only a read method, so a write is impossible by construction; the tests pin what Status answers. The pack
// fixture is a test-only profile (no industry terms in core). All data is SYNTHETIC (AGENT-25).

var cpNow = time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

const cpTTL = time.Hour

type cpPolicy struct {
	terms trustport.ConfirmationTerms
	err   error
}

func (p *cpPolicy) Terms(context.Context, intake.AdmittedIntent) (trustport.ConfirmationTerms, error) {
	return p.terms, p.err
}

type cpStore struct {
	req   trustport.StoredRequest
	found bool
	err   error
	reads int
}

func (s *cpStore) Latest(context.Context, string, string, string, int) (trustport.StoredRequest, bool, error) {
	s.reads++
	return s.req, s.found, s.err
}

type cpWorld struct {
	pol  *cpPolicy
	st   *cpStore
	path trustport.ConfirmationPath
}

func cpAdmitted() intake.AdmittedIntent {
	return intake.AdmittedIntent{TenantID: "ten_1", IntentID: "int_1", Version: 1, IntentNode: "node_1", IntentHash: "sha256:aa", PrincipalID: "prn_1", CorrelationID: "cor_1", TargetRef: "acct_1"}
}

func cpFresh() intake.FreshBinding {
	return intake.FreshBinding{EvidenceSetHash: "sha256:cc", PolicyVersion: "pol_1"}
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
	w := &cpWorld{pol: &cpPolicy{terms: trustport.ConfirmationTerms{Required: true, Requirement: requirement, ExpiresIn: cpTTL}}, st: st}
	p, err := trustport.NewConfirmationPath(trustport.ConfirmationDeps{Policy: w.pol, Store: w.st, Clock: clock.NewManual(cpNow)})
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

func cpNewRequest(requirement string) *intake.RequestSpec {
	return &intake.RequestSpec{Requirement: requirement, TTL: cpTTL}
}

func TestStatusNotRequiredWhenThePolicyAsksForNone(t *testing.T) {
	w := cpBuild(t, "SELF", &cpStore{err: errors.New("must not be read")})
	w.pol.terms.Required = false
	got, err := w.path.Status(cpCtx(t), cpAdmitted(), cpFresh())
	if err != nil || got != (intake.Confirmation{Status: intake.ConfirmationNotRequired}) || w.st.reads != 0 {
		t.Fatalf("got %+v %v reads %d", got, err, w.st.reads)
	}
}

func TestStatusNoRequestYetIsPendingWithANewRequestSpecAndNoID(t *testing.T) {
	w := cpBuild(t, "MAKER_CHECKER", &cpStore{})
	got, err := w.path.Status(cpCtx(t), cpAdmitted(), cpFresh())
	if err != nil || got.Status != intake.ConfirmationPending || got.RequestID != "" || got.EventID != "" || got.NewRequest == nil || *got.NewRequest != *cpNewRequest("MAKER_CHECKER") {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestStatusAnOpenUnexpiredRequestWithNoAnswerIsPendingWithItsIDAndNoNewRequest(t *testing.T) {
	w := cpBuild(t, "SELF", &cpStore{req: cpOpen("SELF"), found: true})
	for i := 0; i < 3; i++ {
		got, err := w.path.Status(cpCtx(t), cpAdmitted(), cpFresh())
		if err != nil || got != (intake.Confirmation{Status: intake.ConfirmationPending, RequestID: "cfr_1"}) {
			t.Fatalf("round %d: %+v %v", i, got, err)
		}
	}
}

func TestStatusAcceptThatResolvesToProceedIsGivenWithEventPlanHashAndRequestID(t *testing.T) {
	w := cpBuild(t, "SELF", &cpStore{req: cpAnswered("SELF", cpAccept("prn_1")), found: true})
	got, err := w.path.Status(cpCtx(t), cpAdmitted(), cpFresh())
	if err != nil || got != (intake.Confirmation{Status: intake.ConfirmationGiven, EventID: "cfe_1", PlanHash: "sha256:dd", RequestID: "cfr_1"}) {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestStatusIsPureAndRepeatable(t *testing.T) {
	w := cpBuild(t, "SELF", &cpStore{req: cpAnswered("SELF", cpAccept("prn_1")), found: true})
	a, errA := w.path.Status(cpCtx(t), cpAdmitted(), cpFresh())
	b, errB := w.path.Status(cpCtx(t), cpAdmitted(), cpFresh())
	if errA != nil || errB != nil || a != b || a.Status != intake.ConfirmationGiven || w.st.reads != 2 {
		t.Fatalf("%+v %v / %+v %v reads %d", a, errA, b, errB, w.st.reads)
	}
}

func TestStatusAGivenAnswerWithAnEmptyFreshValueIsRefusedNeverGiven(t *testing.T) {
	for name, f := range map[string]intake.FreshBinding{
		"no evidence hash":  {PolicyVersion: "pol_1"},
		"no policy version": {EvidenceSetHash: "sha256:cc"},
		"both empty":        {},
	} {
		t.Run(name, func(t *testing.T) {
			w := cpBuild(t, "SELF", &cpStore{req: cpAnswered("SELF", cpAccept("prn_1")), found: true})
			got, err := w.path.Status(cpCtx(t), cpAdmitted(), f)
			if !errors.Is(err, trustport.ErrBadAnswer) || got != (intake.Confirmation{}) {
				t.Fatalf("got %+v %v", got, err)
			}
		})
	}
}

func TestStatusDeclineIsDeclinedWithTheRequestIDEvenWithEmptyFreshValues(t *testing.T) {
	ev := cpAccept("prn_1")
	ev.Response = confirm.Decline
	for _, f := range []intake.FreshBinding{cpFresh(), {}} {
		w := cpBuild(t, "SELF", &cpStore{req: cpAnswered("SELF", ev), found: true})
		got, err := w.path.Status(cpCtx(t), cpAdmitted(), f)
		if err != nil || got != (intake.Confirmation{Status: intake.ConfirmationDeclined, RequestID: "cfr_1"}) {
			t.Fatalf("got %+v %v", got, err)
		}
	}
}

func TestStatusExpiredIsPendingWithANewRequestAndNeverGiven(t *testing.T) {
	late := cpOpen("SELF")
	late.Request.ExpiresAt = cpNow.Add(-time.Second).Unix()
	answeredLate := cpAnswered("SELF", cpAccept("prn_1"))
	answeredLate.Request.ExpiresAt = cpNow.Add(-time.Second).Unix()
	answeredLate.Answer.Event.At = answeredLate.Request.ExpiresAt + 1
	usedLate := cpAnswered("SELF", cpAccept("prn_1"))
	usedLate.Request.ExpiresAt = cpNow.Add(-time.Second).Unix()
	usedLate.Answer.Event.At = usedLate.Request.ExpiresAt - 5
	for name, req := range map[string]trustport.StoredRequest{"no answer, past expiry": late, "answer after expiry": answeredLate, "accepted in time, used after expiry": usedLate} {
		t.Run(name, func(t *testing.T) {
			w := cpBuild(t, "SELF", &cpStore{req: req, found: true})
			got, err := w.path.Status(cpCtx(t), cpAdmitted(), cpFresh())
			if err != nil || got.Status != intake.ConfirmationPending || got.RequestID != "" || got.EventID != "" || got.NewRequest == nil || *got.NewRequest != *cpNewRequest("SELF") {
				t.Fatalf("got %+v %v", got, err)
			}
		})
	}
}

func TestStatusBindingChangedAndPolicyChangedAreAPendingNewRequest(t *testing.T) {
	cases := map[string]func(*intake.FreshBinding){
		"evidence changed": func(f *intake.FreshBinding) { f.EvidenceSetHash = "sha256:ff" },
		"policy changed":   func(f *intake.FreshBinding) { f.PolicyVersion = "pol_2" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			w := cpBuild(t, "SELF", &cpStore{req: cpAnswered("SELF", cpAccept("prn_1")), found: true})
			f := cpFresh()
			mutate(&f)
			got, err := w.path.Status(cpCtx(t), cpAdmitted(), f)
			if err != nil || got.Status != intake.ConfirmationPending || got.RequestID != "" || got.EventID != "" || got.NewRequest == nil || *got.NewRequest != *cpNewRequest("SELF") {
				t.Fatalf("got %+v %v", got, err)
			}
		})
	}
}

func TestStatusNotEligibleAndWrongRequestFailClosed(t *testing.T) {
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
			st, err := w.path.Status(cpCtx(t), cpAdmitted(), cpFresh())
			if !errors.Is(err, c.want) || st != (intake.Confirmation{}) {
				t.Fatalf("status %+v %v", st, err)
			}
		})
	}
}

func TestStatusEligibilityBySelfMakerCheckerAndRole(t *testing.T) {
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
		got, err := w.path.Status(cpCtx(t), cpAdmitted(), cpFresh())
		if c.given && (err != nil || got.Status != intake.ConfirmationGiven) {
			t.Fatalf("%+v: got %+v %v", c, got, err)
		}
		if !c.given && !errors.Is(err, trustport.ErrApproverNotEligible) {
			t.Fatalf("%+v: got %+v %v", c, got, err)
		}
	}
}

// INV-25: a grant can follow only a Proceed. Whatever the stored answer and the fresh values, Given appears exactly when confirm.Resolve says Proceed against the binding
// rebuilt from the stored six values and f, and never otherwise. Seeded, so a failure reproduces.
func TestStatusPropertyNoGivenFromANonProceed(t *testing.T) {
	rng := rand.New(rand.NewSource(25))
	reqs := []string{"SELF", "MAKER_CHECKER", "ROLE:checker", "ROLE:", "BOGUS"}
	pick := func(xs ...string) string { return xs[rng.Intn(len(xs))] }
	for i := 0; i < 600; i++ {
		req := pick(reqs...)
		stored := cpAnswered(req, confirm.Event{RequestID: pick("cfr_1", "cfr_1", "cfr_2"), ApproverID: pick("prn_1", "prn_2", ""), ApproverRoles: []string{pick("checker", "viewer")},
			Response: confirm.Response(pick("ACCEPT", "ACCEPT", "DECLINE", "MAYBE", "")), At: cpNow.Add(time.Duration(rng.Intn(7200)-3600) * time.Second).Unix()})
		stored.Request.ExpiresAt = cpNow.Add(time.Duration(rng.Intn(7200)-3600) * time.Second).Unix()
		w := cpBuild(t, req, &cpStore{req: stored, found: true})
		f := intake.FreshBinding{EvidenceSetHash: pick("sha256:cc", "sha256:c2", ""), PolicyVersion: pick("pol_1", "pol_2", "")}
		got, err := w.path.Status(cpCtx(t), cpAdmitted(), f)
		fresh := cpBinding()
		fresh.EvidenceSetHash, fresh.PolicyVersion = f.EvidenceSetHash, f.PolicyVersion
		res, rerr := confirm.Resolve(stored.Request, stored.Answer.Event, fresh)
		wantGiven := rerr == nil && res == confirm.Proceed && cpNow.Unix() <= stored.Request.ExpiresAt
		isGiven := err == nil && got.Status == intake.ConfirmationGiven
		if isGiven != wantGiven || (isGiven && (got.EventID == "" || got.RequestID != "cfr_1")) {
			t.Fatalf("iteration %d: %+v %+v %+v -> %+v %v; resolve %v %v", i, stored.Request, stored.Answer.Event, f, got, err, res, rerr)
		}
		if err == nil && got.Status == 0 {
			t.Fatalf("iteration %d: a zero status with no error", i)
		}
		if err == nil && got.Status != intake.ConfirmationPending && got.NewRequest != nil {
			t.Fatalf("iteration %d: NewRequest set on %v", i, got.Status)
		}
	}
}

func TestStatusEveryDependencyFailureIsAnErrorNeverPendingOrGiven(t *testing.T) {
	boom := errors.New("down")
	cases := map[string]func(*cpWorld){
		"policy": func(w *cpWorld) { w.pol.err = boom },
		"store":  func(w *cpWorld) { w.st.err = boom },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			w := cpBuild(t, "SELF", &cpStore{req: cpAnswered("SELF", cpAccept("prn_1")), found: true})
			mutate(w)
			if st, err := w.path.Status(cpCtx(t), cpAdmitted(), cpFresh()); !errors.Is(err, boom) || st != (intake.Confirmation{}) {
				t.Fatalf("status %+v %v", st, err)
			}
		})
	}
}

func TestStatusRefusesABadPolicyAndABadAdmittedIntent(t *testing.T) {
	for name, mutate := range map[string]func(*cpWorld){
		"unknown requirement": func(w *cpWorld) { w.pol.terms.Requirement = "ANYONE" },
		"no expiry":           func(w *cpWorld) { w.pol.terms.ExpiresIn = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			w := cpBuild(t, "SELF", &cpStore{})
			mutate(w)
			got, err := w.path.Status(cpCtx(t), cpAdmitted(), cpFresh())
			if !errors.Is(err, trustport.ErrBadAnswer) || got != (intake.Confirmation{}) {
				t.Fatalf("got %+v %v", got, err)
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
			if _, err := w.path.Status(cpCtx(t), a, cpFresh()); !errors.Is(err, trustport.ErrSession) {
				t.Fatalf("%v", err)
			}
		})
	}
	if _, err := trustport.NewConfirmationPath(trustport.ConfirmationDeps{}); !errors.Is(err, trustport.ErrConfig) {
		t.Fatalf("no ports: %v", err)
	}
	var zero trustport.ConfirmationPath
	if _, err := zero.Status(cpCtx(t), cpAdmitted(), cpFresh()); !errors.Is(err, trustport.ErrConfig) {
		t.Fatalf("zero value: %v", err)
	}
}

func TestStatusAStoredRequestOfAnotherPrincipalOrIntentIsANewRequestNeverGiven(t *testing.T) {
	for name, mutate := range map[string]func(*trustport.StoredRequest){
		"principal": func(r *trustport.StoredRequest) { r.Request.Binding.PrincipalID = "prn_9" },
		"requester": func(r *trustport.StoredRequest) { r.Request.RequesterID = "prn_9" },
		"intent":    func(r *trustport.StoredRequest) { r.Request.Binding.IntentHash = "sha256:99" },
	} {
		t.Run(name, func(t *testing.T) {
			req := cpAnswered("SELF", cpAccept("prn_1"))
			mutate(&req)
			w := cpBuild(t, "SELF", &cpStore{req: req, found: true})
			got, err := w.path.Status(cpCtx(t), cpAdmitted(), cpFresh())
			if err != nil || got.Status != intake.ConfirmationPending || got.NewRequest == nil || got.EventID != "" {
				t.Fatalf("got %+v %v", got, err)
			}
		})
	}
}

// The hand-over: what the Recorder persists for a Pending answer with a NewRequest. The requester is the admitted intent's principal (INV-12), never taken from the spec.
func TestNewRequestForCarriesThePrincipalOfTheAdmittedIntentAndTheSpec(t *testing.T) {
	a := cpAdmitted()
	got := trustport.NewRequestFor(a, cpNewRequest("ROLE:checker"))
	want := trustport.NewConfirmationRequest{TenantID: "ten_1", CorrelationID: "cor_1", IntentID: "int_1", IntentVersion: 1, IntentHash: "sha256:aa", RequesterID: "prn_1",
		Requirement: "ROLE:checker", TTL: cpTTL}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}
