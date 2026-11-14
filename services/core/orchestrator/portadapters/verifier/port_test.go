package verifierport_test

// STM-03, A204, REQ-VER-001, AGENT-21, INV-04: the Verifier port over Outcome. The verdict is Outcome's and only Outcome's; the port maps the four verdicts OUT-01
// produces to the contract enum and turns anything else, and every failure, into an error. Identifiers are SYNTHETIC.

import (
	"context"
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	verifierport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/verifier"
	"github.com/Noemai-org/noema/services/core/outcome/verifier"
)

type outcome struct {
	res verifier.Result
	err error
	got verifier.Request
}

func (o *outcome) Verify(_ context.Context, r verifier.Request) (verifier.Result, error) {
	o.got = r
	return o.res, o.err
}

type reqs struct {
	req    verifier.Request
	err    error
	tenant string
	grant  string
}

func (r *reqs) For(_ context.Context, tenantID, grantID string) (verifier.Request, error) {
	r.tenant, r.grant = tenantID, grantID
	return r.req, r.err
}

var sess = orchestrator.Session{TenantID: "ten_aa", PrincipalID: "usr_01"}

func TestTheFourVerdictsMapToTheContractEnumAndTheRequestIsTheGrants(t *testing.T) {
	for in, want := range map[verifier.Verdict]contracts.EnumsV1Verdict{
		verifier.Achieved:        contracts.EnumsV1VerdictACHIEVED,
		verifier.NotAchieved:     contracts.EnumsV1VerdictNOTACHIEVED,
		verifier.PendingExternal: contracts.EnumsV1VerdictPENDINGEXTERNAL,
		verifier.Unknown:         contracts.EnumsV1VerdictUNKNOWN,
	} {
		o, r := &outcome{res: verifier.Result{Verdict: in}}, &reqs{req: verifier.Request{TenantID: "ten_aa", ReceiptID: "rcpt_1"}}
		got, err := verifierport.Port{Verifier: o, Requests: r}.Verify(context.Background(), sess, "dec_AA11", "grt_AA11")
		if err != nil || got != want {
			t.Errorf("%s: got %q, %v; want %q", in, got, err, want)
		}
		if r.tenant != "ten_aa" || r.grant != "grt_AA11" || o.got.ReceiptID != "rcpt_1" {
			t.Errorf("%s: the request source was asked for tenant %q grant %q, Outcome got %+v", in, r.tenant, r.grant, o.got)
		}
	}
}

func TestAnythingElseIsAnErrorNeverAVerdict(t *testing.T) {
	boom := errors.New("synthetic outcome failure")
	good := &reqs{req: verifier.Request{TenantID: "ten_aa"}}
	cases := map[string]verifierport.Port{
		"an unknown verdict":       {Verifier: &outcome{res: verifier.Result{Verdict: "SUCCEEDED"}}, Requests: good},
		"an empty verdict":         {Verifier: &outcome{}, Requests: good},
		"outcome failed":           {Verifier: &outcome{err: boom}, Requests: good},
		"no request":               {Verifier: &outcome{res: verifier.Result{Verdict: verifier.Achieved}}, Requests: &reqs{err: boom}},
		"another tenant's request": {Verifier: &outcome{res: verifier.Result{Verdict: verifier.Achieved}}, Requests: &reqs{req: verifier.Request{TenantID: "ten_bb"}}},
	}
	for name, p := range cases {
		if got, err := p.Verify(context.Background(), sess, "dec_AA11", "grt_AA11"); err == nil || got != "" {
			t.Errorf("%s: got %q, %v; want an error and no verdict", name, got, err)
		}
	}
	for name, call := range map[string]func() error{
		"no tenant": func() error {
			_, e := verifierport.Port{Verifier: &outcome{}, Requests: good}.Verify(context.Background(), orchestrator.Session{PrincipalID: "u"}, "d", "g")
			return e
		},
		"no grant": func() error {
			_, e := verifierport.Port{Verifier: &outcome{}, Requests: good}.Verify(context.Background(), sess, "d", "")
			return e
		},
		"no verifier": func() error {
			_, e := verifierport.Port{Requests: good}.Verify(context.Background(), sess, "d", "g")
			return e
		},
		"no requests": func() error {
			_, e := verifierport.Port{Verifier: &outcome{}}.Verify(context.Background(), sess, "d", "g")
			return e
		},
	} {
		if err := call(); !errors.Is(err, verifierport.ErrNotUsable) {
			t.Errorf("%s: err %v, want ErrNotUsable", name, err)
		}
	}
}
