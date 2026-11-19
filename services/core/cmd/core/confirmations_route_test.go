package main

// TRU-12: POST /v1/confirmations/{confirmation_request_id} (PERD 39.2; REQ-TRU-009, INV-12, INV-25, EC-U04). Body {request_id, response: ACCEPT|DECLINE}. The approver's identity and
// role come from the verified session only; CONFIRMATION_RECEIVED is appended before anything is answered, and an append that fails ends the request. Fakes are SYNTHETIC.
// Safer readings (listed in the PR): a refused reply (not eligible, wrong request, expired) is not recorded; a customer session may answer only a SELF request.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/edge/edgetest"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/trust/confirm"
)

const (
	confTenant    = "ten_aa"
	confRequestID = "cfr_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	confRequester = "prn_req"
)

type confRequests struct {
	req    confirm.Request
	found  bool
	err    error
	calls  int
	tenant string
	id     string
}

func (f *confRequests) Lookup(_ context.Context, tenant, id string) (confirm.Request, bool, error) {
	f.calls++
	f.tenant, f.id = tenant, id
	return f.req, f.found, f.err
}

type confReceipts struct {
	err  error
	got  []ConfirmationReceipt
	fail bool
}

func (f *confReceipts) Receive(_ context.Context, r ConfirmationReceipt) error {
	if f.err != nil {
		return f.err
	}
	f.got = append(f.got, r)
	return nil
}

func confRequest(requirement string) *confRequests {
	return &confRequests{found: true, req: confirm.Request{ID: confRequestID, Requirement: requirement, RequesterID: confRequester, ExpiresAt: apiEpoch.Unix() + 120,
		Binding: confirm.BindingInput{PrincipalID: confRequester, IntentHash: "sha256:a", CapabilityID: "cap.x", TargetRef: "tgt_1", ParamsHash: "sha256:b", EvidenceSetHash: "sha256:c", PlanHash: "sha256:d", PolicyVersion: "pol_1"}}}
}

func confAPI(t *testing.T, iss *edgetest.Issuer, clk *clock.Manual, rq ConfirmationRequests, rc ConfirmationReceipts) http.Handler {
	t.Helper()
	e := sessionEnv(t, iss)
	e[edge.EnvTenants] = confTenant
	cfg := confirmationRouteConfig{Requests: rq, Receipts: rc, Clock: clk, IDs: ids.New(clk, nil), Decode: nil}
	h, err := edge.Load(e.get, os.ReadFile, clk, ids.New(clk, nil), []edge.Route{confirmationRoute(cfg)})
	if err != nil || h == nil {
		t.Fatalf("edge: %v, %v", h, err)
	}
	return h
}

func confToken(iss *edgetest.Issuer, role session.Role, sub string) string {
	c := edgetest.Good(apiEpoch, role)
	c.Aud, c.Tenant, c.Sub = apiAud, confTenant, sub
	return "Bearer " + iss.Token(apiKid, c)
}

func confPost(h http.Handler, authz, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func confAnswer(id, response string) string {
	return `{"request_id":"` + id + `","response":"` + response + `"}`
}

func confSetup(t *testing.T, requirement string) (http.Handler, *edgetest.Issuer, *clock.Manual, *confRequests, *confReceipts) {
	t.Helper()
	iss := edgetest.NewIssuer(t)
	clk := clock.NewManual(apiEpoch)
	rq, rc := confRequest(requirement), &confReceipts{}
	return confAPI(t, iss, clk, rq, rc), iss, clk, rq, rc
}

const confPath = "/v1/confirmations/" + confRequestID

// An approver's ACCEPT is recorded once, with the session's tenant and principal and the session's role never read from the body (INV-12); the answer is 202 and never a grant.
func TestAnAcceptIsRecordedAsTheSessionsApprover(t *testing.T) {
	h, iss, clk, rq, rc := confSetup(t, "MAKER_CHECKER")
	rec := confPost(h, confToken(iss, session.RoleApprover, "prn_apr"), confPath, confAnswer(confRequestID, "ACCEPT"))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if len(rc.got) != 1 {
		t.Fatalf("receipts %d, want 1", len(rc.got))
	}
	g := rc.got[0]
	if g.TenantID != confTenant || g.ApproverID != "prn_apr" || g.RequestID != confRequestID || g.Response != confirm.Accept || !g.At.Equal(clk.Now()) ||
		!regexp.MustCompile(`^cfe_[0-9A-Z]{26}$`).MatchString(g.EventID) || !regexp.MustCompile(`^corr_[0-9A-Z]{26}$`).MatchString(g.CorrelationID) || !regexp.MustCompile(`^[a-z][a-z0-9_]*$`).MatchString(g.Channel) {
		t.Errorf("receipt %+v", g)
	}
	if rq.tenant != confTenant || rq.id != confRequestID {
		t.Errorf("lookup asked %q %q", rq.tenant, rq.id)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get(session.CorrelationHeader) != g.CorrelationID {
		t.Errorf("headers %v", rec.Header())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["state"] == nil || strings.Contains(rec.Body.String(), "grant") {
		t.Errorf("body %s", rec.Body.String())
	}
}

// DECLINE is recorded as DECLINE.
func TestADeclineIsRecorded(t *testing.T) {
	h, iss, _, _, rc := confSetup(t, "MAKER_CHECKER")
	rec := confPost(h, confToken(iss, session.RoleApprover, "prn_apr"), confPath, confAnswer(confRequestID, "DECLINE"))
	if rec.Code != http.StatusAccepted || len(rc.got) != 1 || rc.got[0].Response != confirm.Decline {
		t.Errorf("%d %s %+v", rec.Code, rec.Body.String(), rc.got)
	}
}

// A body that names an identity is refused by the edge and nothing is looked up or recorded; a body field the contract does not have is INPUT_INVALID (INV-12).
func TestBodyIdentityAndUnknownFieldsAreRefused(t *testing.T) {
	h, iss, _, rq, rc := confSetup(t, "MAKER_CHECKER")
	auth := confToken(iss, session.RoleApprover, "prn_apr")
	for name, body := range map[string]string{
		"principal_id":  `{"request_id":"` + confRequestID + `","response":"ACCEPT","principal_id":"prn_req"}`,
		"roles":         `{"request_id":"` + confRequestID + `","response":"ACCEPT","roles":["approver"]}`,
		"approver":      `{"request_id":"` + confRequestID + `","response":"ACCEPT","approver_principal_id":"prn_x"}`,
		"unknown":       `{"request_id":"` + confRequestID + `","response":"ACCEPT","note":"x"}`,
		"bad response":  confAnswer(confRequestID, "MAYBE"),
		"lower case":    confAnswer(confRequestID, "accept"),
		"no response":   `{"request_id":"` + confRequestID + `"}`,
		"no request id": `{"response":"ACCEPT"}`,
		"bad id":        confAnswer("cfr_!!", "ACCEPT"),
		"not an object": `["ACCEPT"]`,
		"trailing":      confAnswer(confRequestID, "ACCEPT") + `{}`,
		"empty":         ``,
	} {
		rec := confPost(h, auth, confPath, body)
		code, _ := errBody(t, rec)
		if rec.Code != http.StatusBadRequest || code != "INPUT_INVALID" {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	if rq.calls != 0 || len(rc.got) != 0 {
		t.Errorf("a refused body reached the store (%d) or the Record (%d)", rq.calls, len(rc.got))
	}
}

// A forged role: the role is the session's. A customer session may answer only a SELF request; an ops or audit session never reaches the handler.
func TestAForgedApproverRoleIsRefused(t *testing.T) {
	h, iss, _, _, rc := confSetup(t, "MAKER_CHECKER")
	rec := confPost(h, confToken(iss, session.RoleCustomer, "prn_other"), confPath, confAnswer(confRequestID, "ACCEPT"))
	if code, _ := errBody(t, rec); rec.Code != http.StatusForbidden || code != "APPROVER_NOT_ELIGIBLE" {
		t.Errorf("customer on maker-checker: %d %s", rec.Code, rec.Body.String())
	}
	h2, iss2, _, _, rc2 := confSetup(t, "ROLE:approver")
	rec = confPost(h2, confToken(iss2, session.RoleCustomer, "prn_other"), confPath, confAnswer(confRequestID, "ACCEPT"))
	if code, _ := errBody(t, rec); rec.Code != http.StatusForbidden || code != "APPROVER_NOT_ELIGIBLE" {
		t.Errorf("customer on ROLE:approver: %d %s", rec.Code, rec.Body.String())
	}
	for _, role := range []session.Role{session.RoleOps, session.RoleAudit} {
		if rec := confPost(h, confToken(iss, role, "prn_apr"), confPath, confAnswer(confRequestID, "ACCEPT")); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", role, rec.Code)
		}
	}
	if rec := confPost(h, "", confPath, confAnswer(confRequestID, "ACCEPT")); rec.Code != http.StatusUnauthorized {
		t.Errorf("no session: %d", rec.Code)
	}
	if len(rc.got)+len(rc2.got) != 0 {
		t.Error("a refused reply was recorded")
	}
}

// Eligibility is trust/confirm's: self-approval under MAKER_CHECKER is refused (EC-U04); SELF is the requester only; ROLE:x needs the session's role. Only an eligible reply is recorded.
func TestEligibilityMatrix(t *testing.T) {
	type row struct {
		requirement string
		role        session.Role
		sub         string
		want        int
	}
	for _, c := range []row{
		{"MAKER_CHECKER", session.RoleApprover, confRequester, http.StatusForbidden},
		{"MAKER_CHECKER", session.RoleApprover, "prn_apr", http.StatusAccepted},
		{"SELF", session.RoleApprover, "prn_apr", http.StatusForbidden},
		{"SELF", session.RoleApprover, confRequester, http.StatusAccepted},
		{"SELF", session.RoleCustomer, confRequester, http.StatusAccepted},
		{"SELF", session.RoleCustomer, "prn_other", http.StatusForbidden},
		{"ROLE:approver", session.RoleApprover, "prn_apr", http.StatusAccepted},
		{"ROLE:treasurer", session.RoleApprover, "prn_apr", http.StatusForbidden},
		{"", session.RoleApprover, "prn_apr", http.StatusForbidden},
	} {
		h, iss, _, _, rc := confSetup(t, c.requirement)
		rec := confPost(h, confToken(iss, c.role, c.sub), confPath, confAnswer(confRequestID, "ACCEPT"))
		recorded := len(rc.got) == 1
		if rec.Code != c.want || recorded != (c.want == http.StatusAccepted) {
			t.Errorf("%+v: %d %s recorded=%v", c, rec.Code, rec.Body.String(), recorded)
		}
		if c.want == http.StatusForbidden {
			if code, _ := errBody(t, rec); code != "APPROVER_NOT_ELIGIBLE" {
				t.Errorf("%+v: code %s", c, code)
			}
		}
	}
}

// The Record comes first: when the append fails nothing else is answered but a 503 with no detail, and a retry is not hidden.
func TestAFailedAppendEndsTheRequest(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	clk := clock.NewManual(apiEpoch)
	rc := &confReceipts{err: errors.New("pgx: connection refused to postgres://record_rw:secret@db")}
	h := confAPI(t, iss, clk, confRequest("MAKER_CHECKER"), rc)
	rec := confPost(h, confToken(iss, session.RoleApprover, "prn_apr"), confPath, confAnswer(confRequestID, "ACCEPT"))
	code, corr := errBody(t, rec)
	if rec.Code != http.StatusServiceUnavailable || code != "DEPENDENCY_UNAVAILABLE" || corr != rec.Header().Get(session.CorrelationHeader) || strings.Contains(rec.Body.String(), "secret") || strings.Contains(rec.Body.String(), "state") {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("an error answer must not be cached")
	}
}

// A replayed ACCEPT is not an error at the route and is handed to the Record as the same reply (same tenant, request, approver and response); the single resolution row and the single
// spend are the store's (confirmation_spend), not the route's.
func TestAReplayedAcceptIsTheSameReply(t *testing.T) {
	h, iss, _, _, rc := confSetup(t, "MAKER_CHECKER")
	auth := confToken(iss, session.RoleApprover, "prn_apr")
	for i := 0; i < 2; i++ {
		if rec := confPost(h, auth, confPath, confAnswer(confRequestID, "ACCEPT")); rec.Code != http.StatusAccepted {
			t.Fatalf("call %d: %d", i, rec.Code)
		}
	}
	a, b := rc.got[0], rc.got[1]
	if a.TenantID != b.TenantID || a.RequestID != b.RequestID || a.ApproverID != b.ApproverID || a.Response != b.Response || a.EventID == b.EventID {
		t.Errorf("replay receipts %+v %+v", a, b)
	}
}

// The wrong request: a body that names another request than the path, or a request of this tenant that is not there, is refused and nothing is recorded. A body naming another
// request is APPROVER_NOT_ELIGIBLE (confirm.WrongRequest); an absent request is the one not-found shape.
func TestTheWrongRequestIsRefused(t *testing.T) {
	h, iss, _, rq, rc := confSetup(t, "MAKER_CHECKER")
	auth := confToken(iss, session.RoleApprover, "prn_apr")
	rec := confPost(h, auth, confPath, confAnswer("cfr_01ARZ3NDEKTSV4RRFFQ69G5FAW", "ACCEPT"))
	if code, _ := errBody(t, rec); rec.Code != http.StatusForbidden || code != "APPROVER_NOT_ELIGIBLE" {
		t.Errorf("mismatch: %d %s", rec.Code, rec.Body.String())
	}
	rq.found = false
	rec = confPost(h, auth, confPath, confAnswer(confRequestID, "ACCEPT"))
	if code, _ := errBody(t, rec); rec.Code != http.StatusNotFound || code != "NOT_FOUND_OR_NOT_AUTHORIZED" {
		t.Errorf("absent: %d %s", rec.Code, rec.Body.String())
	}
	for _, p := range []string{"/v1/confirmations/cfr_!!", "/v1/confirmations/inv_01ARZ3NDEKTSV4RRFFQ69G5FAV", "/v1/confirmations/cfr_" + strings.Repeat("A", 40)} {
		if rec := confPost(h, auth, p, confAnswer(confRequestID, "ACCEPT")); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
	if len(rc.got) != 0 || rq.calls != 1+1 {
		t.Errorf("recorded %d, lookups %d (a malformed path must not reach the store)", len(rc.got), rq.calls)
	}
}

// An expired request is refused (409 CONFIRMATION_EXPIRED) and the reply is not recorded; the instant of expiry itself is still valid, a later one is not.
func TestAnExpiredRequestIsRefused(t *testing.T) {
	h, iss, clk, _, rc := confSetup(t, "MAKER_CHECKER")
	auth := confToken(iss, session.RoleApprover, "prn_apr")
	clk.Advance(120 * 1e9)
	if rec := confPost(h, auth, confPath, confAnswer(confRequestID, "ACCEPT")); rec.Code != http.StatusAccepted {
		t.Fatalf("at the expiry instant: %d", rec.Code)
	}
	clk.Advance(1e9)
	rec := confPost(h, auth, confPath, confAnswer(confRequestID, "ACCEPT"))
	if code, _ := errBody(t, rec); rec.Code != http.StatusConflict || code != "CONFIRMATION_EXPIRED" || len(rc.got) != 1 {
		t.Errorf("after expiry: %d %s recorded %d", rec.Code, rec.Body.String(), len(rc.got))
	}
}

// A store that fails or is missing is 503, never a default.
func TestAFailingOrMissingStoreIs503(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	clk := clock.NewManual(apiEpoch)
	auth := confToken(iss, session.RoleApprover, "prn_apr")
	rq := confRequest("MAKER_CHECKER")
	rq.err = errors.New("pgx: down")
	rc := &confReceipts{}
	for name, h := range map[string]http.Handler{
		"lookup fails": confAPI(t, iss, clk, rq, rc),
		"no requests":  confAPI(t, iss, clk, nil, rc),
		"no receipts":  confAPI(t, iss, clk, confRequest("MAKER_CHECKER"), nil),
	} {
		rec := confPost(h, auth, confPath, confAnswer(confRequestID, "ACCEPT"))
		if code, _ := errBody(t, rec); rec.Code != http.StatusServiceUnavailable || code != "DEPENDENCY_UNAVAILABLE" || strings.Contains(rec.Body.String(), "pgx") {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	if len(rc.got) != 0 {
		t.Error("recorded after a failed lookup")
	}
}

// The decoder is a seam for confirmation-answer.v1: a decoder that refuses is INPUT_INVALID and nothing is looked up.
func TestTheDecoderSeamIsHonoured(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	clk := clock.NewManual(apiEpoch)
	rq, rc := confRequest("MAKER_CHECKER"), &confReceipts{}
	e := sessionEnv(t, iss)
	e[edge.EnvTenants] = confTenant
	cfg := confirmationRouteConfig{Requests: rq, Receipts: rc, Clock: clk, IDs: ids.New(clk, nil),
		Decode: func([]byte) (ConfirmationAnswer, error) { return ConfirmationAnswer{}, errors.New("contract says no") }}
	h, err := edge.Load(e.get, os.ReadFile, clk, ids.New(clk, nil), []edge.Route{confirmationRoute(cfg)})
	if err != nil {
		t.Fatal(err)
	}
	rec := confPost(h, confToken(iss, session.RoleApprover, "prn_apr"), confPath, confAnswer(confRequestID, "ACCEPT"))
	if code, _ := errBody(t, rec); rec.Code != http.StatusBadRequest || code != "INPUT_INVALID" || rq.calls != 0 || len(rc.got) != 0 {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
}
