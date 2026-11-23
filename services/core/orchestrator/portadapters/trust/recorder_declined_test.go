package trustport

// TRU-12, REQ-TRU-009, REQ-STM-001; INV-03 (A470, A507; interim, relayed, not yet committed): the Recorder adapter maps intake.RecordRequest.DeclinedRequestID to
// recorder.Request.DeclinedRequestID for a REJECT DECLINED_BY_APPROVER only. Any other outcome, or another REJECT reason, that names one is refused with ErrRecordRequest before a
// transaction opens; a malformed id is refused the same way. All data is SYNTHETIC. These tests use the adapter's own fakes and run anywhere (no database).

import (
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

const dclRequestID = "cfr_01DECLINED"

// dclRequest is a no-step record request of the given outcome (REJECT, CONFIRM, UNKNOWN, ESCALATE) with the given core reason code (none for CONFIRM), built as the intake builds it.
func dclRequest(t *testing.T, outcome, reason string) intake.RecordRequest {
	t.Helper()
	if outcome == "ALLOW" {
		return recRequest(t, 1)
	}
	b := recBuilder(t)
	res, ok := decide.Admit(decide.IntentView{Version: 2}).Resolved()
	if !ok {
		t.Fatal("not resolved")
	}
	a := decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass}
	var rcs []decide.ReasonCode
	if reason != "" {
		rc, err := decide.Core(reason)
		if err != nil {
			t.Fatal(err)
		}
		rcs = append(rcs, rc)
	}
	var d decide.Decision
	var err error
	switch outcome {
	case "REJECT":
		d, err = res.Reject(a, rcs...)
	case "CONFIRM":
		d, err = res.Confirm(a, rcs...)
	case "UNKNOWN":
		d, err = res.Unknown(a, rcs...)
	case "ESCALATE":
		d, err = res.Escalate(a, rcs...)
	default:
		t.Fatalf("outcome %q", outcome)
	}
	if err != nil {
		t.Fatal(err)
	}
	if d, err = b.Attach(d); err != nil {
		t.Fatal(err)
	}
	digest, err := b.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return intake.RecordRequest{TenantID: recTenant, CorrelationID: "corr_ABC123", PrincipalID: "usr_01J9X3ABCDEFGHJKMNPQRSTVWX", IntentID: "int_01J9X3", IntentHash: recHash(10), IntentVersion: 2,
		Decision: d, Evaluated: b.Sealed(), BasisDigest: digest}
}

func dclRecord(t *testing.T, r intake.RecordRequest, n int) (*recBegin, *recWriter, error) {
	t.Helper()
	b, w := &recBegin{tx: &recTx{}}, &recWriter{res: recGrants(n)}
	_, err := recAdapter(t, b, newRecFacts(), w).Record(recCtx(t), r)
	return b, w, err
}

// A REJECT DECLINED_BY_APPROVER that names its declined request reaches the Writer with the id unchanged (A470).
func TestADeclinedRejectCarriesItsRequestIDToTheWriterUnchanged(t *testing.T) {
	r := dclRequest(t, "REJECT", "DECLINED_BY_APPROVER")
	r.DeclinedRequestID = dclRequestID
	_, w, err := dclRecord(t, r, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.got) != 1 || w.got[0].DeclinedRequestID != dclRequestID {
		t.Fatalf("writer got %+v, want declined request %q", w.got, dclRequestID)
	}
}

// A REJECT with no declined request, for the declined reason or any other, maps an empty value; the rest of the mapping is as before.
func TestARejectWithoutADeclinedRequestMapsEmpty(t *testing.T) {
	for _, reason := range []string{"DECLINED_BY_APPROVER", "NOT_AUTHORIZED", "POLICY_VERSION_MISMATCH"} {
		_, w, err := dclRecord(t, dclRequest(t, "REJECT", reason), 0)
		if err != nil {
			t.Fatalf("%s: %v", reason, err)
		}
		if len(w.got) != 1 || w.got[0].DeclinedRequestID != "" {
			t.Errorf("%s: writer got %+v", reason, w.got)
		}
	}
}

// INV-03: every outcome but a declined REJECT that names a declined request is refused before any transaction, and nothing reaches the Writer.
func TestADeclinedRequestOnAnyOtherDecisionIsRefusedBeforeAnyTransaction(t *testing.T) {
	cases := []struct{ name, outcome, reason string }{
		{"ALLOW", "ALLOW", ""}, {"CONFIRM", "CONFIRM", ""}, {"UNKNOWN", "UNKNOWN", "INTERNAL_ERROR"}, {"ESCALATE", "ESCALATE", "NO_PERMITTING_RULE"},
		{"REJECT for another reason", "REJECT", "NOT_AUTHORIZED"},
	}
	for _, c := range cases {
		r := dclRequest(t, c.outcome, c.reason)
		r.DeclinedRequestID = dclRequestID
		n := 0
		if c.outcome == "ALLOW" {
			n = 1
		}
		b, w, err := dclRecord(t, r, n)
		if !errors.Is(err, ErrRecordRequest) {
			t.Errorf("%s: err %v", c.name, err)
		}
		if len(b.tenants) != 0 || len(w.got) != 0 {
			t.Errorf("%s reached the database", c.name)
		}
	}
}

// A declined request that is not the shape of a confirmation request id is refused before any transaction.
func TestAMalformedDeclinedRequestIDIsRefused(t *testing.T) {
	for _, bad := range []string{"x", "cfr_", "cfr_a", "dec_01DECLINED", "cfr_01 DECLINED", "cfr_01DECLINED\n", "CFR_01DECLINED", "cfr_01DECLINED_EXTRA_LONG_VALUE_BEYOND_THE_LIMIT"} {
		r := dclRequest(t, "REJECT", "DECLINED_BY_APPROVER")
		r.DeclinedRequestID = bad
		b, w, err := dclRecord(t, r, 0)
		if !errors.Is(err, ErrRecordRequest) {
			t.Errorf("%q: err %v", bad, err)
		}
		if len(b.tenants) != 0 || len(w.got) != 0 {
			t.Errorf("%q reached the database", bad)
		}
	}
}
