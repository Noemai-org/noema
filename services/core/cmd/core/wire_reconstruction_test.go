package main

// STM-01 / noema#783, the reconstruction route as ruled (relayed by the user, interim; A110, DEC-2026-11-08, PERD 39.4): audit only, reconstruction.v3, the Record read as the
// SESSION's tenant, 401 for a session of a tenant this core does not serve, the same not-found for a wrong role, an absent correlation and another tenant's, and one
// RECORD_ACCESSED line per read. Every test here runs the production build (buildComponents and buildAPI over the DSN files) on a real PostgreSQL, with a Record written by the
// real appender under each tenant's own logins and read back by each tenant's record_read login. No fault is mocked. Tokens, keys and DSNs are SYNTHETIC (AGENT-25).
// REQ-AUD-003, REQ-API-001, REQ-API-002, REQ-SEC-003, REQ-SEC-004, INV-12.

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/edge/edgetest"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/internal/recordsig"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

const (
	reconShared = "corr_shared01" // in both tenants, with different content
	reconOnlyB  = "corr_bbonly01" // in tenant B only
	reconAbsent = "corr_nothing1" // nowhere
	reconDigits = "corr_0123456789"
)

// zeroEntropy makes test IDs the same on every run (the clock moves one second per event, so they stay unique).
type zeroEntropy struct{}

func (zeroEntropy) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

type reconWorld struct {
	*wireWorld
	clk     *clock.Manual
	app     *chain.Appender
	writers map[string]*pgxpool.Pool
}

// newReconWorld is the two-tenant world of the wire tests with a Record: both tenants hold the correlation corr_shared01 with different content, and tenant B alone holds
// corr_bbonly01.
func newReconWorld(t *testing.T) *reconWorld {
	t.Helper()
	w := newWireWorld(t)
	clk := clock.NewManual(time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC))
	rw := &reconWorld{wireWorld: w, clk: clk, writers: map[string]*pgxpool.Pool{},
		app: &chain.Appender{Clock: clk, IDs: ids.New(clk, zeroEntropy{}), Signers: recordsig.All(t, w.admin, "wirerecon", nil)}}
	rw.story(t, "ten_aa", reconShared, "evr_synthAAAA", "dec_synthAAAA", "SETTLED")
	rw.story(t, "ten_bb", reconShared, "evr_synthBBBB", "dec_synthBBBB", "PENDING")
	rw.story(t, "ten_bb", reconOnlyB, "evr_synthBONLY", "dec_synthBONLY", "PENDING")
	return rw
}

// story appends three events of one correlation, each by its own power's login of the tenant.
func (w *reconWorld) story(t *testing.T, tenant, corr, evr, dec, class string) {
	t.Helper()
	for _, e := range []struct {
		by, group, typ, ref string
		payload             map[string]any
	}{
		{"core-evidence", "evidence_rw", "EVIDENCE_READ", evr, map[string]any{"fact_status": "KNOWN", "source": "mock-ledger"}},
		{"core-trust", "trust_rw", "STATE_RECONCILED", "rec_" + evr[4:], map[string]any{"classification": class, "fact_status": "KNOWN"}},
		{"core-trust", "trust_rw", "DECISION_MADE", dec, map[string]any{"decision_outcome": "ALLOW", "reason_codes": []any{"STATE_KNOWN"}, "decision_id": dec}},
	} {
		key := tenant + "/" + e.group
		if w.writers[key] == nil {
			w.writers[key] = testpg.TenantLogin(t, w.admin, w.admin.Config().ConnString(), e.group, tenant)
		}
		w.clk.Advance(time.Second)
		tx, err := w.writers[key].Begin(deadline(t))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.app.Append(deadline(t), tx, chain.Event{TenantID: tenant, CorrelationID: corr, WrittenBy: e.by, Actor: e.by, EventType: e.typ, ObjectRef: e.ref, Payload: e.payload}); err != nil {
			_ = tx.Rollback(deadline(t))
			t.Fatalf("append %s: %v", e.typ, err)
		}
		if err := tx.Commit(deadline(t)); err != nil {
			t.Fatal(err)
		}
	}
}

// prodLogged builds the components and the protected API the way the process does, with the given logger as the process logger (the access log is made from it).
func (w *reconWorld) prodLogged(t *testing.T, iss *edgetest.Issuer, log *slog.Logger) http.Handler {
	t.Helper()
	_, h := w.prodComponents(t, iss, log)
	return h
}

// prodComponents is prodLogged with the components too: the deployment serves three tenants and only two of them have a Record login (ten_cc has none).
func (w *reconWorld) prodComponents(t *testing.T, iss *edgetest.Issuer, log *slog.Logger) (Components, http.Handler) {
	t.Helper()
	e := w.settings(t)
	for k, v := range sessionEnv(t, iss) {
		e[k] = v
	}
	e[edge.EnvTenants] = "ten_aa,ten_bb,ten_cc"
	clk := clock.NewManual(apiEpoch)
	c, closer, err := buildComponents(t.Context(), osEnv(e), clk, ids.New(clk, nil), log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closer)
	h, err := buildAPI(c, osEnv(e), clk, ids.New(clk, nil))
	if err != nil || h == nil {
		t.Fatalf("api %v, %v", h, err)
	}
	return c, h
}

func recon(h http.Handler, authz, corr string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/v1/reconstructions/"+corr, nil)
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	if mutate != nil {
		mutate(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func accessLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("log line %q: %v", l, err)
		}
		if m["msg"] == "RECORD_ACCESSED" {
			out = append(out, m)
		}
	}
	return out
}

// (a) and (b), REQ-AUD-003, INV-12: the body served is a reconstruction.v3 body of the SESSION's tenant, whatever tenant the request names in its query, headers or body. The
// shared correlation holds different content in the two tenants, so a read as the wrong tenant would show.
func TestTheServedBodyIsReconstructionV3AndTheSessionsTenantNeverTheOneTheRequestNames(t *testing.T) {
	w := newReconWorld(t)
	iss := edgetest.NewIssuer(t)
	h := w.prodLogged(t, iss, discardLog())
	auth := bearerAs(iss, session.RoleAudit, "ten_aa", "prn_9")

	naming := func(r *http.Request) {
		q := r.URL.Query()
		q.Set("tenant_id", "ten_bb")
		q.Set("tenant", "ten_bb")
		r.URL.RawQuery = q.Encode()
		for _, k := range []string{"X-Tenant-Id", "Noema-Tenant-Id", "Tenant-Id", "X-Forwarded-Tenant"} {
			r.Header.Set(k, "ten_bb")
		}
	}
	for name, mutate := range map[string]func(*http.Request){"naming nothing": nil, "naming tenant B everywhere it can": naming} {
		rec := recon(h, auth, reconShared, mutate)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
		var got contracts.ReconstructionV3 // the generated type is closed: an unknown member, a missing one or a value outside its enum or pattern is an error
		dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&got); err != nil {
			t.Fatalf("%s: the served body is not a reconstruction.v3 body: %v", name, err)
		}
		again, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		var a, b any
		if json.Unmarshal(rec.Body.Bytes(), &a) != nil || json.Unmarshal(again, &b) != nil || !reflect.DeepEqual(a, b) {
			t.Errorf("%s: decoding and encoding the body changed it", name)
		}
		body := rec.Body.String()
		if string(got.TenantId) != "ten_aa" || string(got.CorrelationId) != reconShared || len(got.Sections) != 12 || len(got.Events) != 3 || !got.Chain.Verified {
			t.Errorf("%s: tenant %s, correlation %s, %d sections, %d events, verified %v", name, got.TenantId, got.CorrelationId, len(got.Sections), len(got.Events), got.Chain.Verified)
		}
		if !strings.Contains(body, "evr_synthAAAA") || strings.Contains(body, "BBBB") || strings.Contains(body, "ten_bb") || strings.Contains(body, "PENDING") {
			t.Errorf("%s: the body is not tenant A's alone: %s", name, body)
		}
	}
	// A body that names a tenant is refused by the edge's body guard before the handler (INV-12): never tenant B's content, never a read.
	req := httptest.NewRequest(http.MethodGet, "/v1/reconstructions/"+reconShared, io.NopCloser(strings.NewReader(`{"tenant_id":"ten_bb"}`)))
	req.Header.Set("Authorization", auth)
	withBody := httptest.NewRecorder()
	h.ServeHTTP(withBody, req)
	if code, _ := errBody(t, withBody); withBody.Code != http.StatusBadRequest || code != "INPUT_INVALID" || strings.Contains(withBody.Body.String(), "BBBB") {
		t.Errorf("a body naming tenant B: %d %s; want 400 INPUT_INVALID", withBody.Code, withBody.Body.String())
	}
	// Tenant B's own session reads tenant B's content of the same correlation.
	rec := recon(h, bearerAs(iss, session.RoleAudit, "ten_bb", "prn_9"), reconShared, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "evr_synthBBBB") || strings.Contains(rec.Body.String(), "AAAA") {
		t.Errorf("tenant B's session: %d %s", rec.Code, rec.Body.String())
	}
	// A correlation of tenant B only, asked by tenant A's session that names tenant B: not found, and nothing of tenant B.
	rec = recon(h, auth, reconOnlyB, naming)
	if code, _ := errBody(t, rec); rec.Code != http.StatusNotFound || code != "NOT_FOUND_OR_NOT_AUTHORIZED" || strings.Contains(rec.Body.String(), "BONLY") {
		t.Errorf("tenant B's correlation, asked as tenant A naming B: %d %s", rec.Code, rec.Body.String())
	}
}

// (c), REQ-API-002, INV-12: a session of a tenant this core does not serve is refused 401 by the session layer before the route's handler, as a forged token is: one fixed
// answer, and no RECORD_ACCESSED line, because no read happened.
func TestASessionOfATenantTheCoreDoesNotServeIsRefused401BeforeTheHandler(t *testing.T) {
	w := newReconWorld(t)
	iss := edgetest.NewIssuer(t)
	var logBuf bytes.Buffer
	h := w.prodLogged(t, iss, slog.New(slog.NewJSONHandler(&logBuf, nil)))
	foreign := recon(h, bearerAs(iss, session.RoleAudit, "ten_zz", "prn_9"), reconShared, nil)
	forged := recon(h, "Bearer junk", reconShared, nil)
	for name, rec := range map[string]*httptest.ResponseRecorder{"a tenant not served": foreign, "a forged token": forged} {
		if code, _ := errBody(t, rec); rec.Code != http.StatusUnauthorized || code != "SESSION_INVALID" {
			t.Errorf("%s: %d %s; want 401 SESSION_INVALID", name, rec.Code, rec.Body.String())
		}
	}
	if sameButCorrelation(t, foreign, forged) != "" {
		t.Errorf("a foreign tenant's refusal differs from a forged token's: %s | %s", foreign.Body.String(), forged.Body.String())
	}
	if n := len(accessLines(t, &logBuf)); n != 0 {
		t.Errorf("%d RECORD_ACCESSED lines for a request the handler never saw, want 0: %s", n, logBuf.String())
	}
}

// sameButCorrelation compares status, content type and body bytes of two answers once the response's own correlation ID is replaced by a fixed one. It returns "" when they are
// identical and a description otherwise.
func sameButCorrelation(t *testing.T, a, b *httptest.ResponseRecorder) string {
	t.Helper()
	norm := func(rec *httptest.ResponseRecorder) string {
		var env struct {
			Error struct {
				CorrelationID string `json:"correlation_id"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("body %q: %v", rec.Body.String(), err)
		}
		if id := env.Error.CorrelationID; id == "" || rec.Header().Get("Noema-Correlation-Id") != id {
			t.Errorf("the correlation ID of the body %q and of the header %q differ or are empty", id, rec.Header().Get("Noema-Correlation-Id"))
		} else {
			return strings.ReplaceAll(rec.Body.String(), id, "corr_X")
		}
		return rec.Body.String()
	}
	if a.Code != b.Code || a.Header().Get("Content-Type") != b.Header().Get("Content-Type") {
		return "status or content type"
	}
	if na, nb := norm(a), norm(b); na != nb {
		return na + " != " + nb
	}
	return ""
}

// (d), REQ-API-002: a wrong role (customer, approver, ops: the role gate), an absent correlation and another tenant's correlation give one answer: the same status, the same
// content type and the same body bytes, except the correlation ID of the response. The answer does not echo the correlation the caller asked for.
func TestWrongRoleAbsentAndAnotherTenantsCorrelationAreOneNotFound(t *testing.T) {
	w := newReconWorld(t)
	iss := edgetest.NewIssuer(t)
	h := w.prodLogged(t, iss, discardLog())
	audit := bearerAs(iss, session.RoleAudit, "ten_aa", "prn_9")
	cases := map[string]*httptest.ResponseRecorder{
		"a customer":                             recon(h, bearerAs(iss, session.RoleCustomer, "ten_aa", "prn_1"), reconShared, nil),
		"an approver":                            recon(h, bearerAs(iss, session.RoleApprover, "ten_aa", "prn_2"), reconShared, nil),
		"ops":                                    recon(h, bearerAs(iss, session.RoleOps, "ten_aa", "prn_3"), reconShared, nil),
		"audit, an absent correlation":           recon(h, audit, reconAbsent, nil),
		"audit, another tenant's correlation":    recon(h, audit, reconOnlyB, nil),
		"audit, an absent correlation of digits": recon(h, audit, reconDigits, nil),
	}
	for name, rec := range cases {
		if code, _ := errBody(t, rec); rec.Code != http.StatusNotFound || code != "NOT_FOUND_OR_NOT_AUTHORIZED" {
			t.Errorf("%s: %d %s; want 404 NOT_FOUND_OR_NOT_AUTHORIZED", name, rec.Code, rec.Body.String())
		}
		for _, asked := range []string{reconShared, reconOnlyB, reconAbsent, reconDigits} {
			if strings.Contains(rec.Body.String(), asked) || strings.Contains(rec.Header().Get("Noema-Correlation-Id"), asked) {
				t.Errorf("%s: the answer echoes the correlation the caller asked for (%s): %s", name, asked, rec.Body.String())
			}
		}
	}
	ref := cases["a customer"]
	for name, rec := range cases {
		if d := sameButCorrelation(t, ref, rec); d != "" {
			t.Errorf("%s differs from the customer's not-found: %s", name, d)
		}
	}
}

// (e), REQ-AUD-003, REQ-SEC-003, AGENT-13: one RECORD_ACCESSED line for each request that reaches the Record handler, through the redactor, with the tenant, the principal, the
// role, the correlation and the outcome, and nothing of the Record. A request the session layer or the role gate refuses never reaches the handler and writes none.
func TestEachReadThatReachesTheHandlerWritesOneRecordAccessedLineAndNothingElseDoes(t *testing.T) {
	w := newReconWorld(t)
	iss := edgetest.NewIssuer(t)
	audit := bearerAs(iss, session.RoleAudit, "ten_aa", "prn_9")
	for _, c := range []struct {
		name, authz, corr, outcome string // outcome "" means no line
	}{
		{"served", audit, reconShared, "SERVED"},
		{"served again, naming nothing new", audit, reconShared, "SERVED"},
		{"an absent correlation", audit, reconAbsent, "NOT_FOUND_OR_NOT_AUTHORIZED"},
		{"another tenant's correlation", audit, reconOnlyB, "NOT_FOUND_OR_NOT_AUTHORIZED"},
		{"a malformed correlation", audit, "not-a-correlation", "INPUT_INVALID"},
		{"ops (the role gate)", bearerAs(iss, session.RoleOps, "ten_aa", "prn_3"), reconShared, ""},
		{"a customer (the role gate)", bearerAs(iss, session.RoleCustomer, "ten_aa", "prn_1"), reconShared, ""},
		{"a tenant not served (the session layer)", bearerAs(iss, session.RoleAudit, "ten_zz", "prn_9"), reconShared, ""},
		{"no session (the session layer)", "", reconShared, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			var logBuf bytes.Buffer
			h := w.prodLogged(t, iss, slog.New(slog.NewJSONHandler(&logBuf, nil)))
			recon(h, c.authz, c.corr, nil)
			lines := accessLines(t, &logBuf)
			if c.outcome == "" {
				if len(lines) != 0 {
					t.Fatalf("%d RECORD_ACCESSED lines, want none: %s", len(lines), logBuf.String())
				}
				return
			}
			if len(lines) != 1 || strings.Count(logBuf.String(), "RECORD_ACCESSED") != 1 {
				t.Fatalf("%d RECORD_ACCESSED lines, want exactly 1: %s", len(lines), logBuf.String())
			}
			l := lines[0]
			if l["tenant_id"] != "ten_aa" || l["principal"] != "auditor:prn_9" || l["role"] != "audit" || l["outcome"] != c.outcome {
				t.Errorf("fields %v; want tenant ten_aa, principal auditor:prn_9, role audit, outcome %s", l, c.outcome)
			}
			if c.corr == "not-a-correlation" {
				if l["correlation_id"] != "" {
					t.Errorf("correlation_id %v for a malformed ID, want empty", l["correlation_id"])
				}
			} else if l["correlation_id"] != c.corr {
				t.Errorf("correlation_id %v, want %s", l["correlation_id"], c.corr)
			}
			for _, secret := range []string{"evr_synth", "dec_synth", "head_hash", "mock-ledger", "SETTLED", "STATE_KNOWN"} {
				if strings.Contains(logBuf.String(), secret) {
					t.Errorf("the access log carries Record content (%s): %s", secret, logBuf.String())
				}
			}
		})
	}
}

// REQ-SEC-003, AGENT-13, ruling A154: the access log goes through the redactor, so a correlation the caller wrote as a long digit run is logged masked.
func TestTheAccessLogIsRedacted(t *testing.T) {
	w := newReconWorld(t)
	iss := edgetest.NewIssuer(t)
	var logBuf bytes.Buffer
	h := w.prodLogged(t, iss, slog.New(slog.NewJSONHandler(&logBuf, nil)))
	recon(h, bearerAs(iss, session.RoleAudit, "ten_aa", "prn_9"), reconDigits, nil)
	lines := accessLines(t, &logBuf)
	if len(lines) != 1 {
		t.Fatalf("%d lines: %s", len(lines), logBuf.String())
	}
	if got := lines[0]["correlation_id"]; got != "corr_******6789" || strings.Contains(logBuf.String(), "0123456789") {
		t.Errorf("correlation_id %v, line %s; want the digit run masked to its last four", got, logBuf.String())
	}
}

// A434 (interim; DEC-2026-11-20-architect-rulings-a358-a453), REQ-AUD-003, REQ-API-002, AGENT-13: a tenant the core serves but that has no Record login is a deployment defect. Through
// the production build on a real PostgreSQL: a request of that tenant's audit session gets 503 DEPENDENCY_UNAVAILABLE, never a 401 and never a 500, in the bytes of the route's one
// error envelope (the generated type, one encoder, the request's own correlation ID, the asked one never echoed), with fixed text that carries no tenant, login or driver text, and one
// RECORD_ACCESSED line with the outcome code, the role and the principal.
func TestAServedTenantWithNoRecordLoginIs503DependencyUnavailableInTheRoutesOneEnvelope(t *testing.T) {
	w := newReconWorld(t)
	iss := edgetest.NewIssuer(t)
	var logBuf bytes.Buffer
	h := w.prodLogged(t, iss, slog.New(slog.NewJSONHandler(&logBuf, nil)))
	rec := recon(h, bearerAs(iss, session.RoleAudit, "ten_cc", "prn_9"), reconShared, nil)
	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusInternalServerError || rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("%d %s; want 503, never 401 and never 500", rec.Code, rec.Body.String())
	}
	var env contracts.ErrorsV2ErrorEnvelope
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&env); err != nil {
		t.Fatalf("the body is not an errors.v2 envelope: %v", err)
	}
	if string(env.Error.Code) != "DEPENDENCY_UNAVAILABLE" || env.Error.Message != "The Record is not available." || !env.Error.Retryable {
		t.Errorf("code %s, message %q, retryable %v; want DEPENDENCY_UNAVAILABLE, the fixed text, retryable as the contract's code for a dependency", env.Error.Code, env.Error.Message, env.Error.Retryable)
	}
	again, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if string(again)+"\n" != rec.Body.String() {
		t.Errorf("the bytes are not the generated envelope of the one encoder: %q", rec.Body.String())
	}
	if corr := string(env.Error.CorrelationId); corr == "" || rec.Header().Get("Noema-Correlation-Id") != corr || corr == reconShared || rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("correlation %q, header %q, content type %q; want the request's own ID, in both, not the asked one", corr, rec.Header().Get("Noema-Correlation-Id"), rec.Header().Get("Content-Type"))
	}
	for _, leak := range []string{"ten_", "tenant", "login", "pool", "postgres", "record_read", reconShared} {
		if strings.Contains(strings.ToLower(rec.Body.String()), strings.ToLower(leak)) {
			t.Errorf("the answer carries %q: %s", leak, rec.Body.String())
		}
	}
	// The route's other errors are the same shape: status and code and text aside, the same content type and the same key order and encoding.
	nf := recon(h, bearerAs(iss, session.RoleAudit, "ten_aa", "prn_9"), reconAbsent, nil)
	if nf.Code != http.StatusNotFound || nf.Header().Get("Content-Type") != rec.Header().Get("Content-Type") {
		t.Errorf("the not-found: %d, content type %q", nf.Code, nf.Header().Get("Content-Type"))
	}
	lines := accessLines(t, &logBuf)
	if len(lines) != 2 { // the 503 and the not-found
		t.Fatalf("%d RECORD_ACCESSED lines for two requests that reached the handler: %s", len(lines), logBuf.String())
	}
	l := lines[0]
	if l["tenant_id"] != "ten_cc" || l["principal"] != "auditor:prn_9" || l["role"] != "audit" || l["correlation_id"] != reconShared || l["outcome"] != "DEPENDENCY_UNAVAILABLE" {
		t.Errorf("the line %v; want tenant ten_cc, principal auditor:prn_9, role audit, the correlation read and outcome DEPENDENCY_UNAVAILABLE", l)
	}
	// A tenant the core does not serve is still the session layer's 401 and writes no line (A434 leaves it as it is).
	foreign := recon(h, bearerAs(iss, session.RoleAudit, "ten_zz", "prn_9"), reconShared, nil)
	if code, _ := errBody(t, foreign); foreign.Code != http.StatusUnauthorized || code != "SESSION_INVALID" || len(accessLines(t, &logBuf)) != 2 {
		t.Errorf("a tenant not served: %d %s; want 401 SESSION_INVALID and no new line", foreign.Code, foreign.Body.String())
	}
}

// A434: /readyz of the production build is not ready while a served tenant has no Record login, and names the component by a fixed name that carries no tenant ID. The same build with
// a login for every tenant it serves does not name it. The check is the one composeReadiness runs, the one main hands to the mux.
func TestReadyzOfTheProductionBuildFailsForAServedTenantWithNoRecordLogin(t *testing.T) {
	w := newReconWorld(t)
	iss := edgetest.NewIssuer(t)
	c, h := w.prodComponents(t, iss, discardLog()) // serves ten_aa, ten_bb, ten_cc; logins for the first two
	code, body, raw := readyz(t, composeReadiness(c, h))
	if code != http.StatusServiceUnavailable || body["status"] != "not_ready" || !strings.Contains(body["reason"], "record logins") || len(body) != 2 {
		t.Fatalf("status %d, body %s; want 503, not_ready and the record logins named", code, raw)
	}
	for _, leak := range []string{"ten_", "tenant", "postgres", "record_read_", "127.0.0.1"} {
		if strings.Contains(raw, leak) {
			t.Errorf("the body %s carries %q", raw, leak)
		}
	}
	// With the tenants the logins cover, the logins are not named (the ports with no adapter yet still are).
	e := w.settings(t)
	for k, v := range sessionEnv(t, iss) {
		e[k] = v
	}
	e[edge.EnvTenants] = "ten_aa,ten_bb"
	clk := clock.NewManual(apiEpoch)
	ok, closer, err := buildComponents(t.Context(), osEnv(e), clk, ids.New(clk, nil), discardLog())
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	if _, _, raw := readyz(t, composeReadiness(ok, h)); strings.Contains(raw, "record logins") {
		t.Errorf("every served tenant has a login but /readyz names them: %s", raw)
	}
}
