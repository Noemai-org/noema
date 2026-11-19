package main

import (
	"context"
	"net/http"
	"time"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/trust/confirm"
)

// confirmations_route.go: POST /v1/confirmations/{confirmation_request_id} (PERD 39.2, REQ-TRU-009, INV-12, INV-25; task TRU-12). RED stub: the route exists and does nothing yet.

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

func confirmationRoute(c confirmationRouteConfig) edge.Route {
	return edge.Route{Pattern: "POST /v1/confirmations/{confirmation_request_id}", Roles: rolesConfirmers, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
	})}
}

var rolesConfirmers = []session.Role{session.RoleApprover, session.RoleCustomer}
