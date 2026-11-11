package reconstruct_test

// REC-03 slice 2, REQ-AUD-003 ("Ops and audit role only"), REQ-API-004, REQ-SEC-004, INV-12, AGENT-10: the Authorizer of the running core
// takes the caller from the session the edge verified and from nothing else, and the route sits behind the session check, the role gate and
// the database's own tenant binding. Tokens and keys are SYNTHETIC, made per test (AGENT-25).

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

const (
	sessAud = "noema-core"
	sessKid = "idp-rec03-k1"
)

type sessRig struct {
	priv *ecdsa.PrivateKey
	v    *session.Verifier
	now  time.Time
}

func newSessRig(t *testing.T) sessRig {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	v, err := session.NewVerifier(session.Config{Keys: map[string]*ecdsa.PublicKey{sessKid: &k.PublicKey}, Audience: sessAud, Tenants: []string{tenant, other},
		MaxLifetime: session.CeilingLifetime, Skew: time.Minute, Clock: clock.NewManual(now)})
	if err != nil {
		t.Fatal(err)
	}
	return sessRig{priv: k, v: v, now: now}
}

func (r sessRig) token(t *testing.T, sub, tenantID, role string) string {
	t.Helper()
	b64 := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
	h, _ := json.Marshal(map[string]string{"alg": "ES256", "typ": "JWT", "kid": sessKid})
	c, _ := json.Marshal(map[string]any{"sub": sub, "aud": sessAud, "iss": session.Issuer, "tenant_id": tenantID, "roles": []string{role},
		"iat": r.now.Unix(), "exp": r.now.Add(5 * time.Minute).Unix()})
	signing := b64(h) + "." + b64(c)
	sum := sha256.Sum256([]byte(signing))
	rr, ss, err := ecdsa.Sign(rand.Reader, r.priv, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 64)
	rr.FillBytes(sig[:32])
	session.LowS(ss).FillBytes(sig[32:])
	return signing + "." + b64(sig)
}

// route is the reconstruction route as the composition root mounts it: session, then the role gate for ops and audit, then the handler.
func (r sessRig) route(h *reconstruct.Handler, gen session.IDs) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(reconstruct.Route, session.Middleware(r.v, gen)(session.RequireRole(gen, session.RoleOps, session.RoleAudit)(h)))
	return mux
}

func (r sessRig) get(h http.Handler, corr, bearer string) (*httptest.ResponseRecorder, map[string]any) {
	req := httptest.NewRequest(http.MethodGet, "/v1/reconstructions/"+corr, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

// REQ-AUD-003, INV-12: a request with no session principal in its context is refused, and the authorizer never looks at the request's headers
// or query for one.
func TestTheSessionAuthorizerRefusesARequestWithoutASessionPrincipal(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/reconstructions/corr_us06?tenant_id="+tenant+"&principal=operator:usr_x", nil)
	for _, h := range []string{"Noema-Tenant-Id", "X-Principal", "X-Tenant-Id", "Authorization"} {
		req.Header.Set(h, "operator:usr_x")
	}
	if _, err := (reconstruct.SessionAuthorizer{}).Authenticate(req); !errors.Is(err, reconstruct.ErrUnauthenticated) {
		t.Fatalf("got %v, want ErrUnauthenticated", err)
	}
}

// REQ-AUD-003: the route is served to ops and to audit, through the session check and the role gate, with the caller's tenant from the token.
func TestOpsAndAuditSessionsAreServedThroughTheRealEdgePieces(t *testing.T) {
	w := newWorld(t)
	w.append(t, tenant, "corr_us06", us06()...)
	gen := ids.New(clock.NewManual(time.Unix(0, 0)), zeros{})
	rig := newSessRig(t)
	h := rig.route(&reconstruct.Handler{DBs: w.dbs(), Auth: reconstruct.SessionAuthorizer{}, IDs: gen}, gen)
	for _, role := range []string{"ops", "audit"} {
		rec, out := rig.get(h, "corr_us06", rig.token(t, "usr_synth_"+role, tenant, role))
		if rec.Code != http.StatusOK || out["correlation_id"] != "corr_us06" {
			t.Errorf("%s: %d %s", role, rec.Code, rec.Body.String())
		}
	}
}

// REQ-AUD-003, REQ-API-004: customers and approvers never read a reconstruction, no session is 401, a bad token is 401, and a role the gate
// does not list is the answer for "not found", with no events in any of them.
func TestOtherRolesAndNoSessionAreRefusedWithNoEvents(t *testing.T) {
	w := newWorld(t)
	w.append(t, tenant, "corr_us06", us06()...)
	gen := ids.New(clock.NewManual(time.Unix(0, 0)), zeros{})
	rig := newSessRig(t)
	h := rig.route(&reconstruct.Handler{DBs: w.dbs(), Auth: reconstruct.SessionAuthorizer{}, IDs: gen}, gen)
	for name, c := range map[string]struct {
		bearer string
		status int
		code   string
	}{
		"customer":    {rig.token(t, "usr_synth_c", tenant, "customer"), 404, "NOT_FOUND_OR_NOT_AUTHORIZED"},
		"approver":    {rig.token(t, "usr_synth_a", tenant, "approver"), 404, "NOT_FOUND_OR_NOT_AUTHORIZED"},
		"no session":  {"", 401, "UNAUTHENTICATED"},
		"bad session": {"not-a-token", 401, "SESSION_INVALID"},
	} {
		rec, out := rig.get(h, "corr_us06", c.bearer)
		if rec.Code != c.status {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
			continue
		}
		if code, _ := errorCode(t, out); code != c.code {
			t.Errorf("%s: code %s, want %s", name, code, c.code)
		}
	}
}

// Defence in depth: with no role gate in front (a route registered without one by mistake), a customer or approver session is still 404 from
// the handler itself, so the handler does not rely on the gate.
func TestTheHandlerRefusesCustomersAndApproversWithoutTheRoleGate(t *testing.T) {
	w := newWorld(t)
	w.append(t, tenant, "corr_us06", us06()...)
	gen := ids.New(clock.NewManual(time.Unix(0, 0)), zeros{})
	rig := newSessRig(t)
	mux := http.NewServeMux()
	mux.Handle(reconstruct.Route, session.Middleware(rig.v, gen)(&reconstruct.Handler{DBs: w.dbs(), Auth: reconstruct.SessionAuthorizer{}, IDs: gen}))
	for _, role := range []string{"customer", "approver"} {
		rec, out := rig.get(mux, "corr_us06", rig.token(t, "usr_synth_x", tenant, role))
		if rec.Code != 404 {
			t.Fatalf("%s: %d %s", role, rec.Code, rec.Body.String())
		}
		if code, _ := errorCode(t, out); code != "NOT_FOUND_OR_NOT_AUTHORIZED" {
			t.Fatalf("%s: code %s", role, code)
		}
	}
}

// INV-12, REQ-SEC-004: a token of the second tenant reads the second tenant's chain and never the first's, whatever the request says; and when
// the pool handed out for a tenant is another tenant's (a wiring mistake), nothing is served: the chain's genesis commits to the tenant, so the
// read is refused as a failed verification, not served.
func TestATokensTenantDecidesWhichChainIsReadAndAMiswiredPoolServesNothing(t *testing.T) {
	w := newWorld(t)
	w.append(t, tenant, "corr_shared01", us06()...)
	theirs := w.append(t, other, "corr_shared01", us01()...)
	gen := ids.New(clock.NewManual(time.Unix(0, 0)), zeros{})
	rig := newSessRig(t)

	h := rig.route(&reconstruct.Handler{DBs: w.dbs(), Auth: reconstruct.SessionAuthorizer{}, IDs: gen}, gen)
	rec, out := rig.get(h, "corr_shared01?tenant_id="+tenant, rig.token(t, "usr_synth_b", other, "audit"))
	if rec.Code != 200 {
		t.Fatalf("second tenant: %d %s", rec.Code, rec.Body.String())
	}
	body := parse(t, rec.Body.Bytes())
	if len(body.Events) != len(theirs) {
		t.Fatalf("the second tenant's auditor got %d events, want %d (its own)", len(body.Events), len(theirs))
	}
	_ = out

	miswired := dbs{map[string]*pgxpool.Pool{tenant: w.readers[tenant], other: w.readers[tenant]}}
	h = rig.route(&reconstruct.Handler{DBs: miswired, Auth: reconstruct.SessionAuthorizer{}, IDs: gen}, gen)
	rec, out = rig.get(h, "corr_shared01", rig.token(t, "usr_synth_b", other, "audit"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("miswired pool: %d %s", rec.Code, rec.Body.String())
	}
	if code, _ := errorCode(t, out); code != "RECORD_INTEGRITY_FAILURE" {
		t.Fatalf("miswired pool: code %s", code)
	}
}
