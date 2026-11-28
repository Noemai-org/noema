package request_test

// LANE-5 (A723, A740, interim, relayed; REQ-VER-001, REQ-EXE-006; INV-04, INV-12, INV-14, AGENT-13): two more sources of a predicate variable. SUBJECT is the intent's subject, and EPOCH is
// the subject's current logical resolution epoch, read through the Epochs port, as a whole number. A source that cannot be supplied is refused (ErrBinding): the verifier then never
// sees a variable bound to nothing. All data is SYNTHETIC.

import (
	"context"
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/outcome/request"
)

func i64(n int64) *int64 { return &n }

func withSubjectAndEpoch() request.Inputs {
	in := good()
	in.Subject = subject
	in.Epoch = i64(7)
	in.Bindings["subject_ref"] = request.Source{Kind: request.FromSubject}
	in.Bindings["logical_resolution_epoch"] = request.Source{Kind: request.FromEpoch}
	return in
}

func TestSubjectAndEpochAreBoundFromTheInputs(t *testing.T) {
	r := build(t, withSubjectAndEpoch())
	if r.Bindings["subject_ref"] != subject || r.Bindings["logical_resolution_epoch"] != "7" {
		t.Errorf("bindings %v; want subject_ref %q and logical_resolution_epoch 7", r.Bindings, subject)
	}
}

func TestASubjectOrEpochTheInputsCannotSupplyIsRefused(t *testing.T) {
	for name, mut := range map[string]func(*request.Inputs){
		"no subject":     func(in *request.Inputs) { in.Subject = "" },
		"no epoch":       func(in *request.Inputs) { in.Epoch = nil },
		"negative epoch": func(in *request.Inputs) { in.Epoch = i64(-1) },
	} {
		in := withSubjectAndEpoch()
		mut(&in)
		if _, err := request.Build(in); !errors.Is(err, request.ErrBinding) {
			t.Errorf("%s: error %v; want ErrBinding", name, err)
		}
	}
}

type epochsFake struct {
	n      int64
	err    error
	calls  int
	tenant string
	subj   string
}

func (e *epochsFake) Current(_ context.Context, tenantID, subj string) (int64, error) {
	e.calls++
	e.tenant, e.subj = tenantID, subj
	return e.n, e.err
}

func epochRig() *rig {
	r := newRig()
	r.rd.facts.Subject = subject
	r.oc.out.Bindings["subject_ref"] = request.Source{Kind: request.FromSubject}
	r.oc.out.Bindings["logical_resolution_epoch"] = request.Source{Kind: request.FromEpoch}
	return r
}

func (r *rig) callWith(ep request.Epochs) (map[string]string, error) {
	s := r.src()
	s.Epochs = ep
	got, err := s.For(context.Background(), tenant, grantID)
	return got.Bindings, err
}

func TestTheEpochIsReadForTheTenantsSubjectOnceAndBound(t *testing.T) {
	ep := &epochsFake{n: 4}
	b, err := epochRig().callWith(ep)
	if err != nil || b["logical_resolution_epoch"] != "4" || b["subject_ref"] != subject {
		t.Fatalf("bindings %v, err %v; want the epoch 4 and the subject", b, err)
	}
	if ep.calls != 1 || ep.tenant != tenant || ep.subj != subject {
		t.Errorf("epoch port called %d times for (%q, %q); want once for (%q, %q)", ep.calls, ep.tenant, ep.subj, tenant, subject)
	}
}

func TestAnEpochFailureOrAMissingPortGivesNoRequest(t *testing.T) {
	ep := &epochsFake{err: errBoom}
	if b, err := epochRig().callWith(ep); err == nil || len(b) != 0 {
		t.Errorf("a failing epoch read: %v, %v; want an error and no request", b, err)
	}
	if b, err := epochRig().callWith(nil); !errors.Is(err, request.ErrBinding) || len(b) != 0 {
		t.Errorf("no epoch port: %v, %v; want ErrBinding and no request", b, err)
	}
	neg := &epochsFake{n: -3}
	if b, err := epochRig().callWith(neg); err == nil || len(b) != 0 {
		t.Errorf("a negative epoch: %v, %v; want an error and no request", b, err)
	}
}

func TestThePortIsNotAskedWhenNothingBindsAnEpoch(t *testing.T) {
	ep := &epochsFake{n: 4}
	if _, err := newRig().callWith(ep); err != nil {
		t.Fatal(err)
	}
	if ep.calls != 0 {
		t.Errorf("the epoch port was asked %d times for an outcome that binds no epoch", ep.calls)
	}
}
