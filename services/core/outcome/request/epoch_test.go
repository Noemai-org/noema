package request_test

// STM-01, A740 and A776 (interim; REQ-VER-001, REQ-OUT-001; INV-04, INV-12, INV-19): the EPOCH source of an outcome binding. The pack names a variable whose value is the subject's current
// logical-resolution epoch; the request builder binds it from the epoch the caller read through the NextEpoch port (the one the Interpreter uses), and refuses when none was read: a missing
// epoch is never 1 (INV-19). GrantRequests reads the epoch, in the receipt principal's scope, only for an outcome that has an EPOCH binding. All data is SYNTHETIC.

import (
	"context"
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/outcome/request"
	"github.com/Noemai-org/noema/services/core/outcome/verifier"
)

type epochsFake struct {
	n                             int
	err                           error
	calls                         int
	tenant, principal, subjectRef string
}

func (e *epochsFake) EpochOf(_ context.Context, tenantID, principalID, subjectRef string) (int, error) {
	e.calls++
	e.tenant, e.principal, e.subjectRef = tenantID, principalID, subjectRef
	return e.n, e.err
}

func withEpochBinding(in request.Inputs) request.Inputs {
	b := map[string]request.Source{}
	for k, v := range in.Bindings {
		b[k] = v
	}
	b["subject_epoch"] = request.Source{Kind: request.FromEpoch}
	in.Bindings = b
	return in
}

func intp(n int) *int { return &n }

func TestTheBuilderBindsTheEpochItIsGiven(t *testing.T) {
	in := withEpochBinding(good())
	in.Epoch = intp(3)
	if got := build(t, in).Bindings["subject_epoch"]; got != "3" {
		t.Errorf("subject_epoch = %q, want 3", got)
	}
}

func TestAnEpochBindingWithNoEpochOrANonPositiveOneOrAName_IsRefused(t *testing.T) {
	for name, mutate := range map[string]func(*request.Inputs){
		"no epoch read":  func(in *request.Inputs) { in.Epoch = nil },
		"epoch zero":     func(in *request.Inputs) { in.Epoch = intp(0) },
		"epoch negative": func(in *request.Inputs) { in.Epoch = intp(-2) },
		"a name on an epoch source": func(in *request.Inputs) {
			in.Epoch = intp(2)
			in.Bindings["subject_epoch"] = request.Source{Kind: request.FromEpoch, Name: "transaction_ref"}
		},
	} {
		in := withEpochBinding(good())
		mutate(&in)
		if _, err := request.Build(in); !errors.Is(err, request.ErrBinding) {
			t.Errorf("%s: err %v, want ErrBinding", name, err)
		}
	}
}

func TestAnEpochIsNotBoundForABindingThatIsNotAnEpoch(t *testing.T) {
	in := good()
	in.Epoch = intp(9)
	for k, v := range build(t, in).Bindings {
		if v == "9" {
			t.Errorf("binding %s took the epoch", k)
		}
	}
}

func epochRig(n int) (*rig, *epochsFake) {
	r := newRig()
	o := outcome()
	o.Bindings["subject_epoch"] = request.Source{Kind: request.FromEpoch}
	r.oc.out = o
	return r, &epochsFake{n: n}
}

func epochCall(r *rig, e request.Epochs) (verifier.Request, error) {
	s := r.src()
	s.Epochs = e
	return s.For(context.Background(), tenant, grantID)
}

// A740: the epoch is read once, as the receipt's principal in the grant's tenant, for the subject the outcome names, and bound as the variable's value.
func TestAGrantWithAnEpochBindingBindsTheEpochThePortGaveForTheSubject(t *testing.T) {
	r, e := epochRig(4)
	got, err := epochCall(r, e)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if got.Bindings["subject_epoch"] != "4" {
		t.Errorf("subject_epoch = %q, want 4", got.Bindings["subject_epoch"])
	}
	if e.calls != 1 || e.tenant != tenant || e.principal != princ || e.subjectRef != subject {
		t.Errorf("port asked %d times as (%q, %q) for %q; want once as (%q, %q) for %q", e.calls, e.tenant, e.principal, e.subjectRef, tenant, princ, subject)
	}
}

// The port is asked only when the pack's outcome has an EPOCH binding; a pack with none reads no case list at all.
func TestTheEpochPortIsNotAskedForAnOutcomeWithNoEpochBinding(t *testing.T) {
	r := newRig()
	e := &epochsFake{n: 7}
	if _, err := epochCall(r, e); err != nil {
		t.Fatal(err)
	}
	if e.calls != 0 {
		t.Errorf("the port was asked %d times for an outcome with no EPOCH binding", e.calls)
	}
}

// A pack that names an EPOCH binding with no port wired is a named gap: the request is refused and nothing is guessed (INV-19).
func TestAnEpochBindingWithNoPortIsRefusedAsIncomplete(t *testing.T) {
	r, _ := epochRig(4)
	var none request.Epochs
	if got, err := epochCall(r, none); !errors.Is(err, request.ErrIncomplete) || got.ReceiptID != "" {
		t.Errorf("got %+v, %v; want ErrIncomplete and the zero request", got, err)
	}
}

// A port that cannot answer, or answers with no epoch, refuses the request: the cause stays reachable, no epoch is 1 by default.
func TestAPortThatFailsOrNamesNoEpochRefusesTheRequest(t *testing.T) {
	r, e := epochRig(4)
	e.err = errBoom
	if got, err := epochCall(r, e); !errors.Is(err, errBoom) || got.ReceiptID != "" {
		t.Errorf("failing port: got %+v, %v", got, err)
	}
	for _, n := range []int{0, -1} {
		r2, e2 := epochRig(n)
		if got, err := epochCall(r2, e2); !errors.Is(err, request.ErrBinding) || got.ReceiptID != "" {
			t.Errorf("epoch %d: got %+v, %v; want ErrBinding", n, got, err)
		}
	}
}

// The epoch is read only after the receipt, the signature and the facts have passed: a grant that is refused earlier costs no case-list read.
func TestNoEpochIsReadForAGrantThatIsRefusedBeforeTheRequestIsBuilt(t *testing.T) {
	r, e := epochRig(4)
	r.sg.res = verifier.AuthInvalid
	if _, err := epochCall(r, e); err == nil {
		t.Fatal("an unverified receipt was accepted")
	}
	if e.calls != 0 {
		t.Errorf("the port was asked %d times for a refused grant", e.calls)
	}
}
