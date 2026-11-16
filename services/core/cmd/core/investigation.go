package main

import (
	"context"
	"net/http"
	"regexp"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/publicstage"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

// investigation.go: GET /v1/investigations/{id} (PERD 39.2; ruling A243, interim, relayed, not yet committed; noema#601). It answers the current public stage of the caller's own
// investigation: {schema_version, state}. The reader is a port (the read-only store method of A243 is its production form); it answers only for an investigation of this tenant and
// this principal. Nothing is guessed: a state outside the map is an internal error, a store that fails is 503, and every "not yours or not there" is the one not-found shape.

// InvestigationReader returns the internal state of an investigation that belongs to this tenant and this principal, and found=false for any other (absent, another principal's,
// another tenant's). The tenant and principal are the verified session's, never the request's (INV-12).
type InvestigationReader interface {
	Read(ctx context.Context, tenant, principal, id string) (state orchestrator.State, found bool, err error)
}

var investigationPath = regexp.MustCompile(`\Ainv_[0-9A-Za-z]{2,32}\z`)

// investigationRoute is the route, for a customer only. With a nil reader it refuses (503) and never answers a default stage.
func investigationRoute(r InvestigationReader) edge.Route {
	return edge.Route{Pattern: "GET /v1/investigations/{id}", Roles: rolesCustomer, Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		corr := session.CorrelationFromContext(req.Context())
		id := req.PathValue("id")
		p, ok := session.FromContext(req.Context())
		if !ok {
			publicError(w, http.StatusInternalServerError, contracts.ErrorsV3ErrorCodeINTERNALSAFETYBLOCK, "The request cannot be served.", corr)
			return
		}
		if !investigationPath.MatchString(id) {
			publicError(w, http.StatusNotFound, contracts.ErrorsV3ErrorCodeNOTFOUNDORNOTAUTHORIZED, "Not found.", corr)
			return
		}
		if r == nil {
			publicError(w, http.StatusServiceUnavailable, contracts.ErrorsV3ErrorCodeDEPENDENCYUNAVAILABLE, "The service is not available now.", corr)
			return
		}
		state, found, err := r.Read(req.Context(), p.Tenant, p.ID, id)
		if err != nil {
			publicError(w, http.StatusServiceUnavailable, contracts.ErrorsV3ErrorCodeDEPENDENCYUNAVAILABLE, "The service is not available now.", corr)
			return
		}
		if !found {
			publicError(w, http.StatusNotFound, contracts.ErrorsV3ErrorCodeNOTFOUNDORNOTAUTHORIZED, "Not found.", corr)
			return
		}
		stage, err := publicstage.Of(state)
		if err != nil {
			publicError(w, http.StatusInternalServerError, contracts.ErrorsV3ErrorCodeINTERNALSAFETYBLOCK, "The request cannot be served.", corr)
			return
		}
		body, err := crypto.CanonicalJSON(map[string]any{"schema_version": "1.0", "state": string(stage)})
		if err != nil {
			publicError(w, http.StatusInternalServerError, contracts.ErrorsV3ErrorCodeINTERNALSAFETYBLOCK, "The request cannot be served.", corr)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})}
}

// publicError writes the error envelope of errors.v3 with the request's correlation ID and a fixed sentence (AGENT-13). The not-found shape is byte-for-byte the edge's own.
func publicError(w http.ResponseWriter, status int, code contracts.ErrorsV3ErrorCode, message, corr string) {
	body, err := crypto.CanonicalJSON(map[string]any{"error": map[string]any{"code": string(code), "message": message, "retryable": retryableV3(code), "correlation_id": corr}})
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func retryableV3(code contracts.ErrorsV3ErrorCode) bool {
	return code == contracts.ErrorsV3ErrorCodeDEPENDENCYUNAVAILABLE
}
