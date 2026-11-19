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

// ConfirmationAnswerInput is an approver's answer with the identity the session gave (INV-12); At is Unix seconds from the core's clock. It mirrors the store's AnswerInput.
type ConfirmationAnswerInput struct {
	TenantID, RequestID, ApproverID string
	Roles                           []string
	Response                        confirm.Response
	At                              int64
}

// ErrConfirmationAnswered: the request was answered before, differently or by another approver (first answer wins). The store adapter maps its own conflict to it.
var ErrConfirmationAnswered = errors.New("cmd/core: the confirmation request was answered before")

// ConfirmationAnswers is the store's single method: it records the answer and appends CONFIRMATION_RECEIVED in one transaction, returns the event ID, and returns the stored event ID
// for the identical first answer again. A second, different answer is ErrConfirmationAnswered. Answer needs a context deadline.
type ConfirmationAnswers interface {
	Answer(ctx context.Context, a ConfirmationAnswerInput) (string, error)
}

// confirmationRouteConfig is everything the route needs; nothing has a default. Timeout bounds the store's calls.
type confirmationRouteConfig struct {
	Requests ConfirmationRequests
	Answers  ConfirmationAnswers
	Clock    clock.Clock
	Timeout  time.Duration
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
		_ = decode
		publicError(w, http.StatusServiceUnavailable, contracts.ErrorsV3ErrorCodeDEPENDENCYUNAVAILABLE, "The service is not available now.", session.CorrelationFromContext(r.Context()))
	})}
}

var rolesConfirmers = []session.Role{session.RoleApprover, session.RoleCustomer}
