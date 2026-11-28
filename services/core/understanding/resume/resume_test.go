package resume_test

// INP-01, slice 2 (A320, A404, A712; REQ-INP-004; EC-C03; INV-12): the open investigation of a follow-up. A conversation reference `inv_<ULID>` names the investigation whose chain correlation id is
// `corr_<ULID>` (A234). It resolves to the latest admitted intent of THAT investigation, read as the session's tenant, only for the session's own principal. A reference that is malformed, unknown,
// another tenant's or another principal's gets ONE answer (ErrNotFound), so nothing tells them apart; the other principal's case also records a SECURITY_EVENT. An investigation that has ended is
// refused as such, after the ownership check, so another principal learns nothing about it. No database: spies. All data is SYNTHETIC.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/understanding"
	"github.com/Noemai-org/noema/services/core/understanding/intentstore"
	"github.com/Noemai-org/noema/services/core/understanding/resume"
)

var (
	session = understanding.Session{PrincipalID: "usr_88213", TenantID: "ten_demo", AuthnRef: "ses_7a1b", PrincipalType: "end_user", AssertedBy: "noema-poc-idp"}
	errBoom = errors.New("synthetic failure")
)

const (
	ref   = "inv_01J9X3A1B2C3D4E5F6G7H8J9K0"
	corr  = "corr_01J9X3A1B2C3D4E5F6G7H8J9K0"
	actor = "customer:usr_88213"
)

type readerSpy struct {
	got   []string
	admit intentstore.Admitted
	err   error
}

func (r *readerSpy) Latest(_ context.Context, correlationID string) (intentstore.Admitted, error) {
	r.got = append(r.got, correlationID)
	return r.admit, r.err
}

type statesSpy struct {
	ended bool
	err   error
	calls int
	got   string
}

func (s *statesSpy) Terminal(_ context.Context, tenantID, principalID, correlationID string) (bool, error) {
	s.calls++
	s.got = tenantID + "|" + principalID + "|" + correlationID
	return s.ended, s.err
}

type denialsSpy struct {
	got  []string
	err  error
	fail bool
}

func (d *denialsSpy) Denied(_ context.Context, tenantID, a, correlationID string) error {
	d.got = append(d.got, tenantID+"|"+a+"|"+correlationID)
	return d.err
}

func prior(principal string) intentstore.Admitted {
	doc, _ := json.Marshal(map[string]any{"intent_id": "int_1", "version": 2, "principal": map[string]any{"principal_id": principal}})
	return intentstore.Admitted{TenantID: session.TenantID, IntentID: "int_1", Version: 2, CorrelationID: corr, JSON: doc}
}

func world(admit intentstore.Admitted) (resume.Resolver, *readerSpy, *statesSpy, *denialsSpy) {
	rd, st, dn := &readerSpy{admit: admit}, &statesSpy{}, &denialsSpy{}
	return resume.Resolver{Reader: rd, States: st, Denials: dn}, rd, st, dn
}

func TestTheSessionsOwnOpenInvestigationResolvesToItsLatestIntentReadByItsChainCorrelation(t *testing.T) {
	r, rd, st, dn := world(prior(session.PrincipalID))
	got, err := r.Open(context.Background(), session, actor, ref)
	if err != nil {
		t.Fatal(err)
	}
	if want := session.TenantID + "|" + session.PrincipalID + "|" + corr; st.got != want {
		t.Errorf("the state was asked as %q, want the session's tenant and principal and the chain %q", st.got, want)
	}
	if got.IntentID != "int_1" || got.Version != 2 || len(rd.got) != 1 || rd.got[0] != corr || st.calls != 1 || len(dn.got) != 0 {
		t.Errorf("got %+v; reader asked %v; states %d; denials %v", got, rd.got, st.calls, dn.got)
	}
}

func TestAReferenceThatIsNotAnInvestigationIdIsNotFoundAndNothingIsRead(t *testing.T) {
	for _, bad := range []string{"", "inv_", "inv_x", "corr_01J9X3A1B2C3D4E5F6G7H8J9K0", "inv_01J9X3A1B2C3D4E5F6G7H8J9K0\n", "INV_01J9X3A1B2C3D4E5F6G7H8J9K0", "inv_01J9X3A1B2C3D4E5F6G7H8J9K0 "} {
		r, rd, _, _ := world(prior(session.PrincipalID))
		if _, err := r.Open(context.Background(), session, actor, bad); !errors.Is(err, resume.ErrNotFound) || len(rd.got) != 0 {
			t.Errorf("%q: err %v, reads %v; want ErrNotFound and no read", bad, err, rd.got)
		}
	}
}

func TestUnknownAnotherTenantsAndAnotherPrincipalsInvestigationsGiveOneAnswer(t *testing.T) {
	unknown, _, _, _ := world(intentstore.Admitted{})
	unknown.Reader.(*readerSpy).err = intentstore.ErrNotFound
	other, _, _, _ := world(prior("usr_99999"))
	tenant := prior(session.PrincipalID)
	tenant.TenantID = "ten_other"
	foreign, _, _, _ := world(tenant)
	chain := prior(session.PrincipalID)
	chain.CorrelationID = "corr_01JZZZZZZZZZZZZZZZZZZZZZZZ"
	wrong, _, _, _ := world(chain)
	var first error
	for name, r := range map[string]resume.Resolver{"unknown": unknown, "another principal": other, "another tenant": foreign, "another chain": wrong} {
		_, err := r.Open(context.Background(), session, actor, ref)
		if !errors.Is(err, resume.ErrNotFound) {
			t.Errorf("%s: err %v, want ErrNotFound", name, err)
		}
		if first == nil {
			first = err
		} else if err.Error() != first.Error() {
			t.Errorf("%s: the answer %q differs from %q", name, err, first)
		}
	}
}

func TestAnotherPrincipalsInvestigationRecordsASecurityEventAndTheOthersDoNot(t *testing.T) {
	r, _, st, dn := world(prior("usr_99999"))
	if _, err := r.Open(context.Background(), session, actor, ref); !errors.Is(err, resume.ErrNotFound) {
		t.Fatalf("err %v", err)
	}
	if len(dn.got) != 1 || dn.got[0] != session.TenantID+"|"+actor+"|"+corr {
		t.Errorf("denials %v, want one for the session's tenant, actor and the chain", dn.got)
	}
	if st.calls != 0 {
		t.Error("the state of another principal's investigation was asked")
	}
	u, _, _, un := world(intentstore.Admitted{})
	u.Reader.(*readerSpy).err = intentstore.ErrNotFound
	_, _ = u.Open(context.Background(), session, actor, ref)
	if len(un.got) != 0 {
		t.Errorf("an unknown reference raised an event: %v", un.got)
	}
}

func TestAnEventThatCannotBeRecordedStillAnswersNotFoundAndNamesTheRecordFailure(t *testing.T) {
	r, _, _, dn := world(prior("usr_99999"))
	dn.err = errBoom
	_, err := r.Open(context.Background(), session, actor, ref)
	if !errors.Is(err, resume.ErrNotFound) || !errors.Is(err, resume.ErrRecord) || !errors.Is(err, errBoom) {
		t.Errorf("err %v: want ErrNotFound with ErrRecord and the cause reachable", err)
	}
}

func TestAnInvestigationThatHasEndedIsRefusedAsEndedOnlyToItsOwnPrincipal(t *testing.T) {
	r, _, st, _ := world(prior(session.PrincipalID))
	st.ended = true
	if _, err := r.Open(context.Background(), session, actor, ref); !errors.Is(err, resume.ErrTerminal) {
		t.Errorf("own: err %v, want ErrTerminal", err)
	}
	o, _, ost, _ := world(prior("usr_99999"))
	ost.ended = true
	if _, err := o.Open(context.Background(), session, actor, ref); errors.Is(err, resume.ErrTerminal) {
		t.Error("another principal was told the investigation had ended")
	}
}

func TestAStoreOrStateThatCannotAnswerIsAReadFailureAndNeverAnOpenInvestigation(t *testing.T) {
	r, rd, _, _ := world(prior(session.PrincipalID))
	rd.err = errBoom
	if got, err := r.Open(context.Background(), session, actor, ref); !errors.Is(err, resume.ErrRead) || !errors.Is(err, errBoom) || got.IntentID != "" {
		t.Errorf("reader: got %+v, %v", got, err)
	}
	r2, _, st, _ := world(prior(session.PrincipalID))
	st.err = errBoom
	if got, err := r2.Open(context.Background(), session, actor, ref); !errors.Is(err, resume.ErrRead) || got.IntentID != "" {
		t.Errorf("state: got %+v, %v", got, err)
	}
}

func TestAnIncompleteResolverOrSessionIsRefusedBeforeAnyRead(t *testing.T) {
	r, rd, _, _ := world(prior(session.PrincipalID))
	bad := session
	bad.TenantID = ""
	if _, err := r.Open(context.Background(), bad, actor, ref); !errors.Is(err, resume.ErrNotFound) || len(rd.got) != 0 {
		t.Errorf("an invalid session: err %v, reads %v", err, rd.got)
	}
	for name, half := range map[string]resume.Resolver{"no reader": {States: &statesSpy{}, Denials: &denialsSpy{}}, "no states": {Reader: rd, Denials: &denialsSpy{}}, "no denials": {Reader: rd, States: &statesSpy{}}} {
		if _, err := half.Open(context.Background(), session, actor, ref); err == nil || errors.Is(err, resume.ErrNotFound) {
			t.Errorf("%s: err %v, want a configuration error and not a not-found", name, err)
		}
	}
}
