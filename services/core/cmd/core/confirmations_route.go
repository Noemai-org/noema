package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
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

// confirmations_route.go: POST /v1/confirmations/{confirmation_request_id} (PERD 39.2, REQ-TRU-009, INV-12, INV-25; A341, interim, relayed, not yet committed; task TRU-12). The approver's identity and role come from the verified session only (INV-12). The store owns CONFIRMATION_RECEIVED: its Answer records the answer and appends the event in one transaction, and if that fails nothing else happens (PERD 32.4). First answer wins. The route never issues a grant: an ACCEPT leads to a fresh evaluation (INV-25), which is another component's.

// ConfirmationAnswer is the body of the route: {request_id, response}.
type ConfirmationAnswer struct {
	RequestID string
	Response  confirm.Response
}

// ConfirmationAnswerDecoder turns the body into an answer. The default is confirmation-answer.v1's generated type.
type ConfirmationAnswerDecoder func(body []byte) (ConfirmationAnswer, error)

// ConfirmationRequests finds the request a path names, for this tenant only.
type ConfirmationRequests interface {
	Lookup(ctx context.Context, tenant, requestID string) (confirm.StoredRequest, bool, error)
}

// ConfirmationAnswerInput is an approver's answer with the identity the session gave (INV-12); At is Unix seconds from the core's clock. It mirrors the store's AnswerInput.
type ConfirmationAnswerInput struct {
	TenantID, RequestID, ApproverID, ApproverKind string
	Roles                                         []string
	Response                                      confirm.Response
	At                                            int64
}

// ErrConfirmationAnswered: the request was answered before, differently or by another approver (first answer wins). The store adapter maps its own conflict to it.
var ErrConfirmationAnswered = errors.New("cmd/core: the confirmation request was answered before")

// ErrConfirmationExpired: the store's own clock found the request expired when the answer arrived (A607, A368 (iv)); nothing was recorded. The store adapter maps its own error to it.
var ErrConfirmationExpired = errors.New("cmd/core: the confirmation request has expired")

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
	Logger   *slog.Logger // optional: nil logs nothing
}

// Refusal outcomes and their fixed log text: one line per refused answer, carrying the outcome, tenant, request ID (from the path) and the session's principal ID only. Never the body, the answer or a role.
const (
	outcomeNotEligible      = "not_eligible"
	outcomeWrongRequest     = "wrong_request"
	outcomeExpired          = "expired"
	outcomeAlreadyAnswered  = "already_answered"
	msgRefusedNotEligible   = "confirmation answer refused: not eligible"
	msgRefusedWrongRequest  = "confirmation answer refused: wrong request"
	msgRefusedExpired       = "confirmation answer refused: expired"
	msgRefusedAlreadyAnswer = "confirmation answer refused: already answered"
)

func (c confirmationRouteConfig) logRefusal(ctx context.Context, msg, outcome, tenant, requestID, principal string) {
	if c.Logger == nil {
		return
	}
	c.Logger.LogAttrs(ctx, slog.LevelWarn, msg, slog.String("outcome", outcome), slog.String("tenant", tenant), slog.String("request_id", requestID), slog.String("principal_id", principal))
}

var confirmationRequestPath = regexp.MustCompile(`\Acfr_[0-9A-Za-z]{2,32}\z`)

var errConfirmationAnswer = errors.New("cmd/core: the confirmation answer is not valid")

// decodeConfirmationAnswer decodes confirmation-answer.v1 through its generated type (closed object, both members required, the response enum) and adds the request ID's form and
// one document only (INV-12: no member the contract lacks reaches the handler).
func decodeConfirmationAnswer(body []byte) (ConfirmationAnswer, error) {
	var in contracts.ConfirmationAnswerV1
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&in); err != nil {
		return ConfirmationAnswer{}, errConfirmationAnswer
	}
	if _, err := dec.Token(); err != io.EOF {
		return ConfirmationAnswer{}, errConfirmationAnswer
	}
	id := string(in.RequestId)
	if !confirmationRequestPath.MatchString(id) {
		return ConfirmationAnswer{}, errConfirmationAnswer
	}
	switch r := confirm.Response(in.Response); r {
	case confirm.Accept, confirm.Decline:
		return ConfirmationAnswer{RequestID: id, Response: r}, nil
	}
	return ConfirmationAnswer{}, errConfirmationAnswer
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
		if c.Requests == nil || c.Answers == nil || c.Clock == nil || c.Timeout <= 0 {
			publicError(w, http.StatusServiceUnavailable, contracts.ErrorsV3ErrorCodeDEPENDENCYUNAVAILABLE, "The service is not available now.", corr)
			return
		}
		if ans.RequestID != pathID {
			c.logRefusal(r.Context(), msgRefusedWrongRequest, outcomeWrongRequest, p.Tenant, pathID, p.ID)
			publicError(w, http.StatusForbidden, contracts.ErrorsV3ErrorCodeAPPROVERNOTELIGIBLE, "This reply cannot be accepted.", corr)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), c.Timeout) // the earlier of the request's deadline and the configured bound
		defer cancel()
		stored, found, err := c.Requests.Lookup(ctx, p.Tenant, pathID)
		req := stored.Request
		if err != nil {
			publicError(w, http.StatusServiceUnavailable, contracts.ErrorsV3ErrorCodeDEPENDENCYUNAVAILABLE, "The service is not available now.", corr)
			return
		}
		if !found {
			publicError(w, http.StatusNotFound, contracts.ErrorsV3ErrorCodeNOTFOUNDORNOTAUTHORIZED, "Not found.", corr)
			return
		}
		// A607, A612: the request's expiry is the DATABASE's verdict, read with the request; a request at or past its expiry by the database's clock is expired. The application clock decides nothing
		// about expiry and is never rounded up against it.
		if stored.Expired {
			c.logRefusal(ctx, msgRefusedExpired, outcomeExpired, p.Tenant, pathID, p.ID)
			publicError(w, http.StatusConflict, contracts.ErrorsV3ErrorCodeCONFIRMATIONEXPIRED, "This confirmation has expired.", corr)
			return
		}
		at := c.Clock.Now().Unix()
		// The binding is compared by the fresh evaluation, not here: the request's own binding stands in so only the reply, the approver and the time are judged. Identity and roles are the session's.
		res, err := confirm.Resolve(req, confirm.Event{RequestID: ans.RequestID, ApproverID: p.ID, ApproverRoles: []string{string(p.Role)}, Response: ans.Response, At: req.ExpiresAt}, req.Binding) // At: expiry was judged by the database above, so the event is never late here
		if err == nil && p.Role != session.RoleApprover && req.Requirement != "SELF" {
			res = confirm.NotEligible // a customer session answers only a request that asks for the requester themselves
		}
		switch {
		case err != nil:
			publicError(w, http.StatusInternalServerError, contracts.ErrorsV3ErrorCodeINTERNALSAFETYBLOCK, "The request cannot be served.", corr)
			return
		case res == confirm.NotEligible:
			c.logRefusal(ctx, msgRefusedNotEligible, outcomeNotEligible, p.Tenant, pathID, p.ID)
			publicError(w, http.StatusForbidden, contracts.ErrorsV3ErrorCodeAPPROVERNOTELIGIBLE, "This reply cannot be accepted.", corr)
			return
		case res == confirm.WrongRequest:
			c.logRefusal(ctx, msgRefusedWrongRequest, outcomeWrongRequest, p.Tenant, pathID, p.ID)
			publicError(w, http.StatusForbidden, contracts.ErrorsV3ErrorCodeAPPROVERNOTELIGIBLE, "This reply cannot be accepted.", corr)
			return
		case res == confirm.Expired:
			c.logRefusal(ctx, msgRefusedExpired, outcomeExpired, p.Tenant, pathID, p.ID)
			publicError(w, http.StatusConflict, contracts.ErrorsV3ErrorCodeCONFIRMATIONEXPIRED, "This confirmation has expired.", corr)
			return
		case res != confirm.Proceed && res != confirm.Declined:
			publicError(w, http.StatusInternalServerError, contracts.ErrorsV3ErrorCodeINTERNALSAFETYBLOCK, "The request cannot be served.", corr)
			return
		}
		// The store records the answer and appends CONFIRMATION_RECEIVED in one transaction (PERD 32.4: the Record first); the route appends nothing itself. First answer wins.
		_, err = c.Answers.Answer(ctx, ConfirmationAnswerInput{TenantID: p.Tenant, RequestID: pathID, ApproverID: p.ID, ApproverKind: string(p.Role), Roles: []string{string(p.Role)}, Response: ans.Response, At: at})
		switch {
		case errors.Is(err, ErrConfirmationExpired): // the request expired between the read and the store (A368 (iv)); nothing was recorded
			c.logRefusal(ctx, msgRefusedExpired, outcomeExpired, p.Tenant, pathID, p.ID)
			publicError(w, http.StatusConflict, contracts.ErrorsV3ErrorCodeCONFIRMATIONEXPIRED, "This confirmation has expired.", corr)
			return
		case errors.Is(err, ErrConfirmationAnswered):
			c.logRefusal(ctx, msgRefusedAlreadyAnswer, outcomeAlreadyAnswered, p.Tenant, pathID, p.ID)
			publicError(w, http.StatusConflict, contracts.ErrorsV3ErrorCodeIDEMPOTENCYCONFLICT, "This request has already been answered.", corr)
			return
		case err != nil:
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
