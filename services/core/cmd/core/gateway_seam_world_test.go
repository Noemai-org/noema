package main

// STM-06 follow-up (ruling A514, interim; REQ-INT-001, REQ-SEC-004; INV-12, AGENT-10): the world of the gateway-seam identity tests. It drives the REAL chain a verified
// token takes to the Interpreter port, with no database: a signed token, the edge's session middleware and role gate, the Input Gateway, the production Start
// (turnStarter.Start, wire_gateway_start.go), the real Interpreter port (interpreterport.Port) over the production session resolver (edgeSessions, wire_understanding.go) and a
// counting double in the place of the core's Interpreter. The only doubles are the Record of the inputs and the investigation store (which hold nothing the seam reads back), the
// runner (it makes the first call the orchestrator's runner makes, port.Interpret(ctx, session, turn)) and the Interpreter. Every token, key, tenant and identifier is SYNTHETIC.

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/edge/edgetest"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/inputgateway"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	interpreterport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/interpreter"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/understanding"
	"github.com/Noemai-org/noema/services/core/understanding/interpreter"
)

const (
	seamTenantA    = "ten_aa"
	seamTenantB    = "ten_bb"
	seamPrincipalA = "usr_88213"
	seamPrincipalB = "usr_77412"
	seamAuthnRef   = "ses_0123456789abcdef0123456789abcdef"
	seamRecordRef  = "recev_SEAM0000000000000001"
)

// seamRecord is the Record of the inputs without a database: the gateway's in-memory Record (memGateway) plus the admission of each recorded input, which the Interpreter port reads
// back as the orchestrator's Admissions. It answers as the session's tenant: an input of another tenant is not found.
type seamRecord struct {
	*memGateway
	amu      sync.Mutex
	admitted map[string]orchestrator.Admission
}

func newSeamRecord() *seamRecord {
	return &seamRecord{memGateway: &memGateway{prior: map[string]inputgateway.Prior{}}, admitted: map[string]orchestrator.Admission{}}
}

func (r *seamRecord) Received(ctx context.Context, id inputgateway.Identity, corr string, in inputgateway.Received) (inputgateway.Ref, error) {
	ref, err := r.memGateway.Received(ctx, id, corr, in)
	if err != nil {
		return ref, err
	}
	r.admit(ref.EventID, orchestrator.Admission{EventType: "INPUT_RECEIVED", TenantID: id.TenantID, PrincipalID: id.PrincipalID, CorrelationID: corr, RequestID: in.RequestID, ObjectRef: in.ObjectRef})
	return ref, nil
}

func (r *seamRecord) admit(ref string, a orchestrator.Admission) {
	r.amu.Lock()
	defer r.amu.Unlock()
	r.admitted[ref] = a
}

func (r *seamRecord) Lookup(_ context.Context, s orchestrator.Session, ref string) (orchestrator.Admission, error) {
	r.amu.Lock()
	defer r.amu.Unlock()
	a, ok := r.admitted[ref]
	if !ok || a.TenantID != s.TenantID {
		return orchestrator.Admission{}, errors.New("seam: no such recorded input for this tenant")
	}
	return a, nil
}

// seamCore is the core's Interpreter as a counting double: it keeps every input it is handed and answers with a gate refusal, which the port maps to GATE_FAILED with no error.
type seamCore struct {
	mu sync.Mutex
	in []interpreter.Input
}

func (c *seamCore) Interpret(_ context.Context, in interpreter.Input) (interpreter.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.in = append(c.in, in)
	return interpreter.Result{}, &understanding.Rejection{}
}

func (c *seamCore) inputs() []interpreter.Input {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]interpreter.Input(nil), c.in...)
}

// seamPort wraps the real Interpreter port and keeps the orchestrator.Session each call received.
type seamPort struct {
	inner orchestrator.Interpreter
	mu    sync.Mutex
	got   []orchestrator.Session
}

func (p *seamPort) Interpret(ctx context.Context, s orchestrator.Session, t orchestrator.Turn) (orchestrator.Interpretation, error) {
	p.mu.Lock()
	p.got = append(p.got, s)
	p.mu.Unlock()
	return p.inner.Interpret(ctx, s, t)
}

func (p *seamPort) sessions() []orchestrator.Session {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]orchestrator.Session(nil), p.got...)
}

// seamRunner makes the call the orchestrator's runner makes first (WriteRunner.RunWrite: Interpreter.Interpret(ctx, session, turn)) and nothing after it.
type seamRunner struct{ port orchestrator.Interpreter }

func (r seamRunner) RunWrite(ctx context.Context, s orchestrator.Session, t orchestrator.Turn) (orchestrator.Result, error) {
	_, err := r.port.Interpret(ctx, s, t)
	return orchestrator.Result{}, err
}

type seamWorld struct {
	iss     *edgetest.Issuer
	h       http.Handler
	starter *turnStarter
	store   *fakeStarter
	rec     *seamRecord
	port    *seamPort
	core    *seamCore
	fails   *failures
}

func seamPortOver(t *testing.T, core interpreterport.Interpreter, rec *seamRecord) *seamPort {
	t.Helper()
	req, err := interpreterport.RequiredFromPack([]byte(`{"intent_nodes":[{"node_id":"node_a","slots":[]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	p, err := interpreterport.New(interpreterport.Config{Sessions: edgeSessions{}, Interpreter: core, Clock: clock.NewManual(apiEpoch), PackRef: "demo@1.0.0", Required: req, Admissions: rec})
	if err != nil {
		t.Fatal(err)
	}
	return &seamPort{inner: p}
}

// newSeamWorld builds the chain over the production Start, the production resolver and the real port. Call drain before reading what the workers saw.
func newSeamWorld(t *testing.T) *seamWorld {
	t.Helper()
	w := &seamWorld{iss: edgetest.NewIssuer(t), rec: newSeamRecord(), core: &seamCore{}, store: &fakeStarter{created: true}, fails: &failures{}}
	w.port = seamPortOver(t, w.core, w.rec)
	stores := fakeStores{by: map[string]*fakeStarter{seamTenantA: w.store, seamTenantB: w.store}}
	st, err := newTurnStarter(t.Context(), cfg(1, 8), stores, seamRunner{port: w.port}, w.fails.report)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Shutdown)
	w.starter = st
	clk := clock.NewManual(apiEpoch)
	g, err := inputgateway.New(inputgateway.Config{Recorder: w.rec, Index: w.rec, Start: st.Start, IDs: fixedIDs{ids.New(clk, nil)}, MaxBody: 16384, HashKey: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	e := sessionEnv(t, w.iss)
	e[edge.EnvMaxBody] = "16384"
	e[edge.EnvTenants] = seamTenantA + "," + seamTenantB
	h, err := buildAPI(Components{Gateway: g}, Env{Getenv: e.get, ReadFile: os.ReadFile}, clk, ids.New(clk, nil))
	if err != nil || h == nil {
		t.Fatalf("buildAPI: %v, %v", h, err)
	}
	w.h = h
	return w
}

// drain lets every accepted turn finish (the pool's own shutdown) and closes the pool: the world takes no more turns after it.
func (w *seamWorld) drain() { w.starter.Shutdown() }

// send posts one request to the conversation route. Headers and the raw query are the request's own; none is part of the token.
func (w *seamWorld) send(authz, body string, headers map[string]string, rawQuery string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/conversations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.URL.RawQuery = rawQuery
	rec := httptest.NewRecorder()
	w.h.ServeHTTP(rec, req)
	return rec
}

// seamVerifier is the edge's own verifier over the issuer's key: the oracle of what a token says, independent of the code under test (it is the edge's verification, not the gateway
// start or the resolver).
func seamVerifier(t *testing.T, iss *edgetest.Issuer, tenants ...string) *session.Verifier {
	t.Helper()
	v, err := session.NewVerifier(session.Config{Keys: map[string]*ecdsa.PublicKey{apiKid: iss.Public()}, Audience: apiAud, Tenants: tenants, MaxLifetime: 10 * time.Minute, Skew: time.Minute, Clock: clock.NewManual(apiEpoch)})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// seamToken signs a token for the tenant, subject and role and returns it as an Authorization value with the principal the edge's verifier reads from it.
func seamToken(t *testing.T, iss *edgetest.Issuer, role session.Role, tenant, sub string) (string, session.Principal) {
	t.Helper()
	c := edgetest.Good(apiEpoch, role)
	c.Aud, c.Tenant, c.Sub = apiAud, tenant, sub
	tok := iss.Token(apiKid, c)
	p, err := seamVerifier(t, iss, tenant).Verify(tok)
	if err != nil {
		t.Fatal(err)
	}
	if p.Tenant != tenant || p.ID != sub || p.Role != role || p.AssertedBy != session.Issuer || !sessionAuthnRefRe.MatchString(p.AuthnRef) || strings.Contains(tok, p.AuthnRef) {
		t.Fatalf("the verified principal is not the token's: %+v", p)
	}
	return "Bearer " + tok, p
}

// seamWantSession is the orchestrator.Session the verified principal stands for: every member from the token and the edge's own mapping, none from anything else.
func seamWantSession(p session.Principal) orchestrator.Session {
	return orchestrator.Session{TenantID: p.Tenant, PrincipalID: p.ID, Role: string(p.Role), AuthnRef: p.AuthnRef, PrincipalType: p.PrincipalType, AssertedBy: p.AssertedBy}
}

// seamWantInterpreterSession is the understanding.Session the Interpreter must be handed for the verified principal.
func seamWantInterpreterSession(p session.Principal) understanding.Session {
	return understanding.Session{PrincipalID: p.ID, TenantID: p.Tenant, AuthnRef: p.AuthnRef, PrincipalType: p.PrincipalType, AssertedBy: p.AssertedBy}
}

// seamAssertIdentity is the one check of the seam: the store was asked once as the verified session, the port received exactly that orchestrator.Session, and the Interpreter was
// handed exactly one understanding.Session of the verified principal, with the actor and the canonical input of the same identity. No run reported a failure.
func seamAssertIdentity(t *testing.T, w *seamWorld, p session.Principal) {
	t.Helper()
	want := seamWantSession(p)
	if w.store.calls != 1 || w.store.sess != want {
		t.Errorf("the investigation store was asked %d times as %+v; want once as %+v", w.store.calls, w.store.sess, want)
	}
	if got := w.port.sessions(); len(got) != 1 || got[0] != want {
		t.Errorf("the port received %+v; want exactly %+v", got, want)
	}
	in := w.core.inputs()
	if len(in) != 1 {
		t.Fatalf("the Interpreter was called %d times; want once", len(in))
	}
	if in[0].Session != seamWantInterpreterSession(p) || in[0].Actor != p.PrincipalType+":"+p.ID {
		t.Errorf("the Interpreter was handed %+v as %q; want %+v as %q", in[0].Session, in[0].Actor, seamWantInterpreterSession(p), p.PrincipalType+":"+p.ID)
	}
	var ci struct {
		TenantID     string `json:"tenant_id"`
		SessionID    string `json:"session_id"`
		PrincipalRef string `json:"principal_ref"`
	}
	if err := json.Unmarshal(in[0].Request.CanonicalInput, &ci); err != nil || ci.TenantID != p.Tenant || ci.SessionID != p.AuthnRef || ci.PrincipalRef != p.ID {
		t.Errorf("the canonical input names %+v (%v); want the verified tenant, session reference and principal", ci, err)
	}
	if len(w.fails.errs) != 0 {
		t.Errorf("a run reported a failure: %v", w.fails.errs)
	}
}

// seamNothingRan asserts that nothing past the refusal ran: no investigation was asked for, no session reached the port and the Interpreter was never called.
func seamNothingRan(t *testing.T, w *seamWorld, what string) {
	t.Helper()
	if w.store.calls != 0 || len(w.port.sessions()) != 0 || len(w.core.inputs()) != 0 {
		t.Errorf("%s: the store was asked %d times, the port received %d sessions and the Interpreter was called %d times; want none", what, w.store.calls, len(w.port.sessions()), len(w.core.inputs()))
	}
}
