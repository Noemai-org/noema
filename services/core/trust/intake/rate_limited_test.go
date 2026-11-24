package intake

// TRU-11 (REQ-TRU-012, REQ-TRU-004; PERD 23.3 T11, 23.5 reason RATE_LIMITED; INV-03, INV-12, AGENT-16): the policy layer refuses a write whose principal has used up the rate of a
// capability: REJECT RATE_LIMITED, with no grant and no write. The limit and the count belong to the Rates port's adapter (pack data and the database clock); Trust holds no number.
// Every value is SYNTHETIC.

import (
	"context"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/policy"
)

// rateFake is the Rates port: the capabilities over their rate, an optional fault, and what it was asked.
type rateFake struct {
	over  map[string]bool
	err   error
	panic bool
	asked []RateQuery
}

func (r *rateFake) Exceeded(_ context.Context, q RateQuery) (bool, error) {
	r.asked = append(r.asked, q)
	if r.panic {
		panic("injected panic in Exceeded")
	}
	return r.over[q.CapabilityID], r.err
}

// writeEv is an evaluation of a write whose authorized set is the given capabilities, with the Rates port wired and a PERMIT waiting at the policy.
func writeEv(t *testing.T, caps []string, rates Rates) (*evaluation, *fake) {
	t.Helper()
	f := &fake{pol: decided(policy.Permit, ""), authorized: caps}
	ev := newEv(t, f)
	ev.late.needsWrite = true
	ev.svc.Rates = rates
	return ev, f
}

func TestAPrincipalOverTheRateOfACapabilityIsRejectedRateLimitedAtT11(t *testing.T) {
	rf := &rateFake{over: map[string]bool{tCreateCap: true}}
	ev, f := writeEv(t, []string{tCreateCap}, rf)
	d, trace := run(t, ev, decide.T11Policy)
	stopped(t, "over the rate", d, trace, decide.T11Policy, decide.Reject, []string{"RATE_LIMITED"}, rejectLayers)
	if f.called("Evaluate") {
		t.Error("the policy was evaluated for a principal that is over the rate: the rate check comes first and ends the evaluation")
	}
}

func TestAnyCapabilityOfTheSetOverItsRateRejectsTheWrite(t *testing.T) {
	rf := &rateFake{over: map[string]bool{tWriteCap: true}}
	ev, _ := writeEv(t, []string{tCreateCap, tWriteCap}, rf)
	d, trace := run(t, ev, decide.T11Policy)
	stopped(t, "second capability over", d, trace, decide.T11Policy, decide.Reject, []string{"RATE_LIMITED"}, rejectLayers)
	if len(rf.asked) == 0 {
		t.Fatal("the port was never asked")
	}
}

func TestAPrincipalUnderTheRateGoesOnToThePolicyAndIsAskedOnceForEachCapabilityOfTheSet(t *testing.T) {
	rf := &rateFake{}
	ev, f := writeEv(t, []string{tCreateCap, tWriteCap}, rf)
	_, trace := run(t, ev, decide.T11Policy)
	wentOn(t, "under the rate", trace, decide.T11Policy)
	want := []RateQuery{{tTenant, "usr_synth01", tWriteCap}, {tTenant, "usr_synth01", tCreateCap}} // sorted: adjust_record before create_case
	if !reflect.DeepEqual(rf.asked, want) {
		t.Errorf("asked %+v, want %+v: the tenant and the principal of the session, one question for each capability of the sorted set", rf.asked, want)
	}
	if !f.called("Evaluate") {
		t.Error("the policy was not evaluated for a principal under the rate")
	}
}

func TestARequestThatLeadsToNoWriteIsNeverRateChecked(t *testing.T) {
	rf := &rateFake{over: map[string]bool{"": true, tCreateCap: true}}
	f := &fake{pol: decided(policy.Permit, "")}
	ev := newEv(t, f).withCapability(tCreateCap)
	ev.svc.Rates = rf // a read: needsWrite is false
	_, trace := run(t, ev, decide.T11Policy)
	wentOn(t, "a read", trace, decide.T11Policy)
	if len(rf.asked) != 0 {
		t.Errorf("a read asked the rate port: %+v", rf.asked)
	}
}

func TestWithNoRatesPortNoCapabilityIsLimited(t *testing.T) {
	ev, _ := writeEv(t, []string{tCreateCap}, nil)
	_, trace := run(t, ev, decide.T11Policy)
	wentOn(t, "no port", trace, decide.T11Policy)
}

func TestTheRateCheckFailsClosedOnAnErrorAndAPanic(t *testing.T) {
	for name, rf := range map[string]*rateFake{"an error": {err: errFault}, "a panic": {panic: true}} {
		ev, f := writeEv(t, []string{tCreateCap}, rf)
		d, trace := run(t, ev, decide.T11Policy)
		failedClosed(t, name, d, trace, decide.T11Policy)
		if f.called("Evaluate") {
			t.Errorf("%s: the policy was evaluated after the rate check failed", name)
		}
	}
}
