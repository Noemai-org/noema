package session_test

// REC-02 (REQ-AUD-004): the correlation ID the edge mints is also in the telemetry chain of the request, so the model client sends it and the bound logger logs it. A
// caller's own Noema-Correlation-Id is never the chain's. Synthetic values only.

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/platform/telemetry"
)

func TestTheMintedCorrelationIDIsInTheTelemetryChain(t *testing.T) {
	k := newKeys(t)
	v, clk := verifier(t, k, epoch)
	var inChain string
	h := session.Middleware(v, ids.New(clk, nil))(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		inChain = telemetry.CorrelationFrom(r.Context())
		if inChain != session.CorrelationFromContext(r.Context()) {
			t.Errorf("the chain %q and the edge %q disagree", inChain, session.CorrelationFromContext(r.Context()))
		}
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/conversations", nil)
	req.Header.Set("Authorization", "Bearer "+mint(t, k.priv, goodHeader(), goodClaims(epoch)))
	req.Header.Set(session.CorrelationHeader, "corr_0123456789")
	h.ServeHTTP(rec, req)
	if inChain == "" || inChain == "corr_0123456789" || inChain != rec.Header().Get(session.CorrelationHeader) {
		t.Fatalf("chain id %q, header %q", inChain, rec.Header().Get(session.CorrelationHeader))
	}
}
