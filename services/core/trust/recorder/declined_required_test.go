package recorder_test

// TRU-12, ruling A597 and A600 (interim; A542; REQ-TRU-009, INV-03, INV-12, EC-E16): the Writer refuses a REJECT DECLINED_BY_APPROVER that names no declined request (an empty
// DeclinedRequestID) with ErrRequest, before it sends a single statement on the transaction it was given: nothing is locked, read or written for a decline that rests on no request.
// Every other REJECT stays free to name none. All data is SYNTHETIC.

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

// noStatementTx is a transaction on which any call is a failure of the test: the embedded interface is nil, so a method call panics, and the test reports it as a statement sent.
type noStatementTx struct{ pgx.Tx }

func TestADeclinedRejectWithNoDeclinedRequestIDIsRefusedBeforeAnyStatement(t *testing.T) {
	w := newWorld(t)
	r := declinedReject(t, "evs_01ABC", "") // the REJECT DECLINED_BY_APPROVER of the tuple, naming no request
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("a statement was sent on the transaction before the decline with no request was refused (%v)", p)
		}
	}()
	_, err := w.w.Write(ctx(t), noStatementTx{}, r)
	if !errors.Is(err, recorder.ErrRequest) {
		t.Fatalf("err %v, want ErrRequest", err)
	}
}

// The same refusal path of Writer.check as above, on a request ID that is not shaped like one (a malformed DeclinedRequestID): refused with ErrRequest before any statement. It is the
// regression case of the build break at 317fe93 (noema#998): the refusals of check return the four values, so the package builds and each refusal reaches the caller as ErrRequest.
func TestADeclinedRejectNamingAMalformedRequestIDIsRefusedBeforeAnyStatement(t *testing.T) {
	w := newWorld(t)
	r := declinedReject(t, "evs_01ABC", "not a request id")
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("a statement was sent on the transaction before the decline naming a malformed request was refused (%v)", p)
		}
	}()
	_, err := w.w.Write(ctx(t), noStatementTx{}, r)
	if !errors.Is(err, recorder.ErrRequest) {
		t.Fatalf("err %v, want ErrRequest", err)
	}
}
