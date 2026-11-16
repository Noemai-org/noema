package main

// EDG-01 slice 3c, ruling A234 (interim; docs#187, relayed, not yet committed): POST /v1/conversations answers the shape of PERD 39.2 plus the correlation ID.
// `investigation_id` is `inv_` and the ULID of the input's core-minted correlation ID, so a retry gets the same value (A168); `conversation_ref` is the same ULID with its
// contract prefix (`canonical-input.v2` types it as an InvestigationId, `inv_`: PERD 32.3 has no other prefix for it); `state` is `understanding`; `stream_url` is the events route of
// that conversation. Nothing internal (the event ID and sequence of the input) leaves. The route is mounted behind the verified session and the customer role gate, and only when the
// Input Gateway is configured. Errors pass through unchanged. REQ-API-001, REQ-INP-001 to REQ-INP-003, INV-12. Fakes below are SYNTHETIC; the Record is the gateway's own tests'.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/edge/edgetest"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/inputgateway"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
)

const (
	aRequestID = "3f2a9c1e-5b7d-4e8f-9a1c-2d4b6e8f0a1b"
	aULID      = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
)

// memGateway is the gateway's world without a database: an in-memory Record of the inputs, and a seam that counts its calls and can fail.
type memGateway struct {
	mu      sync.Mutex
	prior   map[string]inputgateway.Prior
	started []inputgateway.Turn
	failing bool
	n       int
}

func (m *memGateway) Received(_ context.Context, id inputgateway.Identity, corr string, in inputgateway.Received) (inputgateway.Ref, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.n++
	ref := inputgateway.Ref{EventID: "recev_" + aULID[:20] + string(rune('A'+m.n)) + "AA", Seq: int64(m.n)}
	m.prior[id.TenantID+"|"+id.PrincipalID+"|"+in.RequestID] = inputgateway.Prior{Ref: ref, CorrelationID: corr, InputHash: in.InputHash}
	return ref, nil
}
func (m *memGateway) Deduplicated(context.Context, inputgateway.Identity, string, string, inputgateway.Ref) error {
	return nil
}
func (m *memGateway) Reused(context.Context, inputgateway.Identity, string, string, string, inputgateway.Ref) error {
	return nil
}
func (m *memGateway) Find(_ context.Context, id inputgateway.Identity, requestID string) (inputgateway.Prior, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.prior[id.TenantID+"|"+id.PrincipalID+"|"+requestID]
	return p, ok, nil
}
func (m *memGateway) start(_ context.Context, t inputgateway.Turn) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = append(m.started, t)
	if m.failing {
		return context.DeadlineExceeded
	}
	return nil
}

// fixedIDs mints the ULID of the correlation ID, so a test knows what the route must derive.
type fixedIDs struct{ inner session.IDs }

func (f fixedIDs) ID(prefix string) (string, error) {
	if prefix == "corr" {
		return "corr_" + aULID, nil
	}
	return f.inner.ID(prefix)
}

func newGateway(t *testing.T, m *memGateway) *inputgateway.Gateway {
	t.Helper()
	clk := clock.NewManual(apiEpoch)
	g, err := inputgateway.New(inputgateway.Config{Recorder: m, Index: m, Start: m.start, IDs: fixedIDs{ids.New(clk, nil)}, MaxBody: 16384, HashKey: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func conversationAPI(t *testing.T, iss *edgetest.Issuer, c Components) http.Handler {
	t.Helper()
	clk := clock.NewManual(apiEpoch)
	e := sessionEnv(t, iss)
	e[edge.EnvMaxBody] = "16384"
	h, err := buildAPI(c, Env{Getenv: e.get, ReadFile: os.ReadFile}, clk, ids.New(clk, nil))
	if err != nil || h == nil {
		t.Fatalf("buildAPI: %v, %v", h, err)
	}
	return h
}

func startBody(requestID, text string) string {
	return `{"request_id":"` + requestID + `","channel":"text","text":"` + text + `","locale_hint":"en-NG"}`
}

func decodeStart(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return got
}

// A234: the PERD shape plus correlation_id, exactly, and nothing internal.
func TestAnAcceptedInputAnswersThePERDShapePlusTheCorrelationID(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	m := &memGateway{prior: map[string]inputgateway.Prior{}}
	h := conversationAPI(t, iss, Components{Gateway: newGateway(t, m)})
	rec := post(h, bearer(iss, session.RoleCustomer, edgetest.Tenant), startBody(aRequestID, "where is my money"))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	got := decodeStart(t, rec)
	want := map[string]any{"investigation_id": "inv_" + aULID, "conversation_ref": "inv_" + aULID, "state": "understanding", "stream_url": "/v1/conversations/inv_" + aULID + "/events", "correlation_id": "corr_" + aULID}
	if len(got) != len(want) {
		t.Fatalf("members %v; want exactly %v (nothing internal: no event ID, sequence or hash)", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content type %q", ct)
	}
	if len(m.started) != 1 {
		t.Errorf("the turn was started %d times, want 1", len(m.started))
	}
}

// A168, A234: a retry of the same input gets 200 and the same values; Start is called again for the same recorded input (the seam is idempotent per input).
func TestARetryOfTheSameInputGetsTheSameInvestigation(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	m := &memGateway{prior: map[string]inputgateway.Prior{}}
	h := conversationAPI(t, iss, Components{Gateway: newGateway(t, m)})
	auth := bearer(iss, session.RoleCustomer, edgetest.Tenant)
	first := post(h, auth, startBody(aRequestID, "where is my money"))
	retry := post(h, auth, startBody(aRequestID, "where is my money"))
	if first.Code != http.StatusAccepted || retry.Code != http.StatusOK {
		t.Fatalf("statuses %d and %d, want 202 and 200", first.Code, retry.Code)
	}
	a, b := decodeStart(t, first), decodeStart(t, retry)
	for _, k := range []string{"investigation_id", "conversation_ref", "state", "stream_url", "correlation_id"} {
		if a[k] != b[k] {
			t.Errorf("%s differs on a retry: %v and %v", k, a[k], b[k])
		}
	}
	if len(a) != 5 || len(b) != 5 {
		t.Errorf("members %v and %v; want five each", a, b)
	}
}

// Errors are the gateway's, unchanged: a body that is not valid is 400 INPUT_INVALID, a Start that fails is 503 DEPENDENCY_UNAVAILABLE, in the error envelope and with none of the
// success members.
func TestAnErrorPassesThroughUnchanged(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	m := &memGateway{prior: map[string]inputgateway.Prior{}}
	h := conversationAPI(t, iss, Components{Gateway: newGateway(t, m)})
	auth := bearer(iss, session.RoleCustomer, edgetest.Tenant)
	bad := post(h, auth, `{"request_id":"nope","channel":"text","text":"x"}`)
	if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), "INPUT_INVALID") || strings.Contains(bad.Body.String(), "investigation_id") {
		t.Errorf("an invalid body: %d %s", bad.Code, bad.Body.String())
	}
	m.failing = true
	down := post(h, auth, startBody("5c1d0e2a-7b3f-4a6e-8d9c-1e2f3a4b5c6d", "x"))
	if down.Code != http.StatusServiceUnavailable || !strings.Contains(down.Body.String(), "DEPENDENCY_UNAVAILABLE") || strings.Contains(down.Body.String(), "investigation_id") {
		t.Errorf("a Start that fails: %d %s", down.Code, down.Body.String())
	}
}

// The route exists for a customer only, and only when the gateway is configured: ops, audit and approver get 404, no session gets 401, and with no gateway there is no route.
func TestTheRouteIsMountedForACustomerAndOnlyWithAGateway(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	m := &memGateway{prior: map[string]inputgateway.Prior{}}
	with := conversationAPI(t, iss, Components{Gateway: newGateway(t, m)})
	for _, role := range []session.Role{session.RoleOps, session.RoleAudit, session.RoleApprover} {
		if rec := post(with, bearer(iss, role, edgetest.Tenant), startBody(aRequestID, "x")); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", role, rec.Code)
		}
	}
	if rec := post(with, "", startBody(aRequestID, "x")); rec.Code != http.StatusUnauthorized {
		t.Errorf("no session: %d, want 401", rec.Code)
	}
	if len(m.started) != 0 {
		t.Errorf("a refused request started %d turns", len(m.started))
	}
	// no gateway: no route (the Record reader makes the API buildable, as the reconstruction route does)
	without := conversationAPI(t, iss, Components{Records: &failingDBs{}, AccessLog: discardLog()})
	if rec := post(without, bearer(iss, session.RoleCustomer, edgetest.Tenant), startBody(aRequestID, "x")); rec.Code != http.StatusNotFound {
		t.Errorf("no gateway: %d, want 404", rec.Code)
	}
}

// The identity the gateway records is the session's (INV-12): the Turn it hands the seam names the token's tenant and principal and the customer kind.
func TestTheTurnNamesTheSessionsTenantAndPrincipal(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	m := &memGateway{prior: map[string]inputgateway.Prior{}}
	h := conversationAPI(t, iss, Components{Gateway: newGateway(t, m)})
	post(h, bearer(iss, session.RoleCustomer, edgetest.Tenant), startBody(aRequestID, "where is my money"))
	if len(m.started) != 1 {
		t.Fatalf("%d turns", len(m.started))
	}
	id := m.started[0].Identity
	if id.TenantID != edgetest.Tenant || id.PrincipalID != "prn_1" || id.PrincipalKind != "customer" {
		t.Errorf("identity %+v", id)
	}
}

// The derivation: inv_ and the ULID of the correlation ID, nothing else. A correlation ID that is not corr_ and a ULID is refused (the route then answers an error, never a guess).
func TestTheInvestigationIDIsInvAndTheULIDOfTheCorrelationID(t *testing.T) {
	if got, err := investigationIDOf("corr_" + aULID); err != nil || got != "inv_"+aULID {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, bad := range []string{"", "corr_", "corr_NONE", "inv_" + aULID, "corr_" + aULID + "0", "corr_" + strings.ToLower(aULID), "corr_01ARZ3NDEKTSV4RRFFQ69G5FAI", "corr_" + aULID[:25], "corr_ " + aULID[1:]} {
		if got, err := investigationIDOf(bad); err == nil {
			t.Errorf("%q gave %q: want an error", bad, got)
		}
	}
}
