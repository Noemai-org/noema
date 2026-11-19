package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/trust/confirm"
)

// confirmations_route.go: POST /v1/confirmations/{confirmation_request_id} (PERD 39.2, REQ-TRU-009, INV-12, INV-25; task TRU-12). The approver's identity and role come from the verified session only (INV-12). CONFIRMATION_RECEIVED is appended before anything is answered; if the append fails nothing else happens (PERD 32.4). The route never issues a grant: an ACCEPT leads to a fresh evaluation (INV-25), which is another component's.

// ConfirmationAnswer is the body of the route: {request_id, response}.
type ConfirmationAnswer struct {
	RequestID string
	Response  confirm.Response
}

// ConfirmationAnswerDecoder turns the body into an answer. It is the seam where confirmation-answer.v1 (C1) is enforced once that contract is on main.
type ConfirmationAnswerDecoder func(body []byte) (ConfirmationAnswer, error)

// ConfirmationRequests finds the request a path names, for this tenant only.
type ConfirmationRequests interface {
	Lookup(ctx context.Context, tenant, requestID string) (confirm.Request, bool, error)
}

// ConfirmationReceipt is one CONFIRMATION_RECEIVED event to append.
type ConfirmationReceipt struct {
	TenantID, CorrelationID, EventID, RequestID, ApproverID, Channel string
	Response                                                         confirm.Response
	At                                                               time.Time
}

// ConfirmationReceipts appends CONFIRMATION_RECEIVED to the Record. An error means nothing was recorded.
type ConfirmationReceipts interface {
	Receive(ctx context.Context, r ConfirmationReceipt) error
}

// confirmationRouteConfig is everything the route needs; nothing has a default.
type confirmationRouteConfig struct {
	Requests ConfirmationRequests
	Receipts ConfirmationReceipts
	Clock    clock.Clock
	IDs      session.IDs
	Decode   ConfirmationAnswerDecoder
}

// confirmationChannel is the channel of the recorded event (confirmation-event.v1: ^[a-z][a-z0-9_]*$).
const confirmationChannel = "public_api"

var confirmationRequestPath = regexp.MustCompile(`\Acfr_[0-9A-Za-z]{2,32}\z`)

var errConfirmationAnswer = errors.New("cmd/core: the confirmation answer is not valid")

// decodeConfirmationAnswer is the stand-in for confirmation-answer.v1 (C1): exactly the two members, no unknown member, one document, a request ID in the contract's form and
// one of the two responses. It is replaced by the generated type when that contract is on main.
func decodeConfirmationAnswer(body []byte) (ConfirmationAnswer, error) {
	var in struct {
		RequestID *string `json:"request_id"`
		Response  *string `json:"response"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return ConfirmationAnswer{}, errConfirmationAnswer
	}
	if _, err := dec.Token(); err != io.EOF {
		return ConfirmationAnswer{}, errConfirmationAnswer
	}
	if in.RequestID == nil || in.Response == nil || !confirmationRequestPath.MatchString(*in.RequestID) {
		return ConfirmationAnswer{}, errConfirmationAnswer
	}
	switch r := confirm.Response(*in.Response); r {
	case confirm.Accept, confirm.Decline:
		return ConfirmationAnswer{RequestID: *in.RequestID, Response: r}, nil
	}
	return ConfirmationAnswer{}, errConfirmationAnswer
}

// replyTime is the reply's instant in Unix seconds, rounded up: a sub-second error can make a reply late, never early.
func replyTime(t time.Time) int64 {
	at := t.Unix()
	if t.Nanosecond() > 0 {
		at++
	}
	return at
}

// confirmationRoute is the route. Roles: approver and customer (a customer answers only a SELF request, below); ops and audit never reach the handler.
func confirmationRoute(c confirmationRouteConfig) edge.Route {
	decode := c.Decode
	if decode == nil {
		decode = decodeConfirmationAnswer
	}
	return edge.Route{Pattern: "POST /v1/confirmations/{confirmation_request_id}", Roles: rolesConfirmers, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		corr := session.CorrelationFromContext(r.Context())
		p, ok := session.FromContext(r.Context())
		if !ok {
			publicError(w, http.StatusInternalServerError, contracts.ErrorsV3ErrorCodeINTERNALSAFETYBLOCK, "The request cannot be served.", corr)
			return
		}
		pathID := r.PathValue("confirmation_request_id")
		if !confirmationRequestPath.MatchString(pathID) {
			publicError(w, http.StatusNotFound, contracts.ErrorsV3ErrorCodeNOTFOUNDORNOTAUTHORIZED, "Not found.", corr)
			return
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			publicError(w, http.StatusBadRequest, contracts.ErrorsV3ErrorCodeINPUTINVALID, "The request is not acceptable.", corr)
			return
		}
		ans, err := decode(raw)
		if err != nil {
			publicError(w, http.StatusBadRequest, contracts.ErrorsV3ErrorCodeINPUTINVALID, "The request is not acceptable.", corr)
			return
		}
		if c.Requests == nil || c.Receipts == nil || c.Clock == nil || c.IDs == nil {
			publicError(w, http.StatusServiceUnavailable, contracts.ErrorsV3ErrorCodeDEPENDENCYUNAVAILABLE, "The service is not available now.", corr)
			return
		}
		if ans.RequestID != pathID {
			publicError(w, http.StatusForbidden, contracts.ErrorsV3ErrorCodeAPPROVERNOTELIGIBLE, "This reply cannot be accepted.", corr)
			return
		}
		req, found, err := c.Requests.Lookup(r.Context(), p.Tenant, pathID)
		if err != nil {
			publicError(w, http.StatusServiceUnavailable, contracts.ErrorsV3ErrorCodeDEPENDENCYUNAVAILABLE, "The service is not available now.", corr)
			return
		}
		if !found {
			publicError(w, http.StatusNotFound, contracts.ErrorsV3ErrorCodeNOTFOUNDORNOTAUTHORIZED, "Not found.", corr)
			return
		}
		now := c.Clock.Now()
		// The binding is compared by the fresh evaluation, not here: the request's own binding stands in so only the reply, the approver and the time are judged.
		res, err := confirm.Resolve(req, confirm.Event{RequestID: ans.RequestID, ApproverID: p.ID, ApproverRoles: []string{string(p.Role)}, Response: ans.Response, At: replyTime(now)}, req.Binding)
		if err == nil && p.Role != session.RoleApprover && req.Requirement != "SELF" {
			res = confirm.NotEligible // a customer session answers only a request that asks for the requester themselves
		}
		switch {
		case err != nil:
			publicError(w, http.StatusInternalServerError, contracts.ErrorsV3ErrorCodeINTERNALSAFETYBLOCK, "The request cannot be served.", corr)
			return
		case res == confirm.NotEligible || res == confirm.WrongRequest:
			publicError(w, http.StatusForbidden, contracts.ErrorsV3ErrorCodeAPPROVERNOTELIGIBLE, "This reply cannot be accepted.", corr)
			return
		case res == confirm.Expired:
			publicError(w, http.StatusConflict, contracts.ErrorsV3ErrorCodeCONFIRMATIONEXPIRED, "This confirmation has expired.", corr)
			return
		case res != confirm.Proceed && res != confirm.Declined:
			publicError(w, http.StatusInternalServerError, contracts.ErrorsV3ErrorCodeINTERNALSAFETYBLOCK, "The request cannot be served.", corr)
			return
		}
		eventID, err := c.IDs.ID("cfe")
		if err != nil {
			publicError(w, http.StatusInternalServerError, contracts.ErrorsV3ErrorCodeINTERNALSAFETYBLOCK, "The request cannot be served.", corr)
			return
		}
		// The Record comes first: nothing is answered but an error when the append fails.
		if err := c.Receipts.Receive(r.Context(), ConfirmationReceipt{TenantID: p.Tenant, CorrelationID: corr, EventID: eventID, RequestID: pathID, ApproverID: p.ID,
			Channel: confirmationChannel, Response: ans.Response, At: now}); err != nil {
			publicError(w, http.StatusServiceUnavailable, contracts.ErrorsV3ErrorCodeDEPENDENCYUNAVAILABLE, "The service is not available now.", corr)
			return
		}
		out, err := crypto.CanonicalJSON(map[string]any{"schema_version": "1.0", "state": "received"})
		if err != nil {
			publicError(w, http.StatusInternalServerError, contracts.ErrorsV3ErrorCodeINTERNALSAFETYBLOCK, "The request cannot be served.", corr)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write(out)
	})}
}

var rolesConfirmers = []session.Role{session.RoleApprover, session.RoleCustomer}
