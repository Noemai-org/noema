package main

// TRU-12 (A341): POST /v1/confirmations/{confirmation_request_id} (PERD 39.2; REQ-TRU-009, INV-12, INV-25, EC-U04). Body {request_id, response: ACCEPT|DECLINE}. The approver's identity and
// role come from the verified session only. The STORE owns CONFIRMATION_RECEIVED: the route calls the store's one Answer method, which records the answer and appends the event in one
// transaction; the route appends nothing itself. FIRST ANSWER WINS: a second answer is 409, the identical first answer again returns the stored result. Fakes are SYNTHETIC.
// Safer readings (listed in the PR): a refused reply (not eligible, wrong request, expired) is not recorded; a customer session may answer only a SELF request; a second answer is
// IDEMPOTENCY_CONFLICT.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpg2"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

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

// confAnswers is a SYNTHETIC store with the first-answer-wins rule: one answer per request; the same approver with the same response again returns the stored event ID.
type confAnswers struct {
	err   error
	got   []ConfirmationAnswerInput
	first *ConfirmationAnswerInput
	event string
	calls int
	noDL  bool
}

func (f *confAnswers) Answer(ctx context.Context, a ConfirmationAnswerInput) (string, error) {
	f.calls++
	if _, ok := ctx.Deadline(); !ok {
		f.noDL = true
	}
	if f.err != nil {
		return "", f.err
	}
	if f.first != nil {
		if f.first.ApproverID == a.ApproverID && f.first.Response == a.Response {
			return f.event, nil
		}
		return "", ErrConfirmationAnswered
	}
	f.first, f.event = &a, "cfe_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	f.got = append(f.got, a)
	return f.event, nil
}

func confRequest(requirement string) *confRequests {
	return &confRequests{found: true, req: confirm.Request{ID: confRequestID, Requirement: requirement, RequesterID: confRequester, ExpiresAt: apiEpoch.Unix() + 120,
		Binding: confirm.BindingInput{PrincipalID: confRequester, IntentHash: "sha256:a", CapabilityID: "cap.x", TargetRef: "tgt_1", ParamsHash: "sha256:b", EvidenceSetHash: "sha256:c", PlanHash: "sha256:d", PolicyVersion: "pol_1"}}}
}

func confAPI(t *testing.T, iss *edgetest.Issuer, clk *clock.Manual, rq ConfirmationRequests, rc ConfirmationAnswers) http.Handler {
	t.Helper()
	e := sessionEnv(t, iss)
	e[edge.EnvTenants] = confTenant
	cfg := confirmationRouteConfig{Requests: rq, Answers: rc, Clock: clk, Timeout: time.Second, Decode: nil}
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

func confSetup(t *testing.T, requirement string) (http.Handler, *edgetest.Issuer, *clock.Manual, *confRequests, *confAnswers) {
	t.Helper()
	iss := edgetest.NewIssuer(t)
	clk := clock.NewManual(apiEpoch)
	rq, rc := confRequest(requirement), &confAnswers{}
	return confAPI(t, iss, clk, rq, rc), iss, clk, rq, rc
}

const confPath = "/v1/confirmations/" + confRequestID

// An approver's ACCEPT is handed to the store once (the store records it and appends CONFIRMATION_RECEIVED), with the session's tenant, principal and role, never read from the body (INV-12); the answer is 202 and never a grant.
func TestAnAcceptIsRecordedAsTheSessionsApprover(t *testing.T) {
	h, iss, clk, rq, rc := confSetup(t, "MAKER_CHECKER")
	rec := confPost(h, confToken(iss, session.RoleApprover, "prn_apr"), confPath, confAnswer(confRequestID, "ACCEPT"))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if len(rc.got) != 1 || rc.calls != 1 {
		t.Fatalf("answers %d (calls %d), want 1", len(rc.got), rc.calls)
	}
	g := rc.got[0]
	if g.TenantID != confTenant || g.ApproverID != "prn_apr" || g.RequestID != confRequestID || g.Response != confirm.Accept || g.At != clk.Now().Unix() || len(g.Roles) != 1 || g.Roles[0] != "approver" {
		t.Errorf("answer %+v", g)
	}
	if rc.noDL {
		t.Error("the store was called without a context deadline")
	}
	if rq.tenant != confTenant || rq.id != confRequestID {
		t.Errorf("lookup asked %q %q", rq.tenant, rq.id)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get(session.CorrelationHeader) == "" {
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
	rc := &confAnswers{err: errors.New("pgx: connection refused to postgres://record_rw:secret@db")}
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

// FIRST ANSWER WINS (A341). The identical first answer by the same approver again returns the stored result (202, idempotent) and stores nothing new.
func TestAReplayedAcceptReturnsTheStoredResult(t *testing.T) {
	h, iss, _, _, rc := confSetup(t, "MAKER_CHECKER")
	auth := confToken(iss, session.RoleApprover, "prn_apr")
	var first string
	for i := 0; i < 2; i++ {
		rec := confPost(h, auth, confPath, confAnswer(confRequestID, "ACCEPT"))
		if rec.Code != http.StatusAccepted {
			t.Fatalf("call %d: %d", i, rec.Code)
		}
		if i == 0 {
			first = rec.Body.String()
		} else if rec.Body.String() != first {
			t.Errorf("replay body %s, first %s", rec.Body.String(), first)
		}
	}
	if len(rc.got) != 1 || rc.calls != 2 {
		t.Errorf("stored %d, calls %d", len(rc.got), rc.calls)
	}
}

// FIRST ANSWER WINS: a second answer to the same request is 409 IDEMPOTENCY_CONFLICT, from another approver, or from the same approver with the other response; the first stands.
func TestASecondAnswerIs409(t *testing.T) {
	for name, second := range map[string]struct{ sub, resp string }{
		"another approver, same response":  {"prn_apr2", "ACCEPT"},
		"another approver, other response": {"prn_apr2", "DECLINE"},
		"same approver, other response":    {"prn_apr", "DECLINE"},
	} {
		h, iss, _, _, rc := confSetup(t, "MAKER_CHECKER")
		if rec := confPost(h, confToken(iss, session.RoleApprover, "prn_apr"), confPath, confAnswer(confRequestID, "ACCEPT")); rec.Code != http.StatusAccepted {
			t.Fatalf("%s: first %d", name, rec.Code)
		}
		rec := confPost(h, confToken(iss, session.RoleApprover, second.sub), confPath, confAnswer(confRequestID, second.resp))
		if code, _ := errBody(t, rec); rec.Code != http.StatusConflict || code != "IDEMPOTENCY_CONFLICT" || strings.Contains(rec.Body.String(), "state\":\"received") {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
		if len(rc.got) != 1 || rc.first.ApproverID != "prn_apr" || rc.first.Response != confirm.Accept {
			t.Errorf("%s: the first answer did not stand: %+v", name, rc.first)
		}
	}
}

// An ineligible second answerer is refused as ineligible (403) before the store is asked; the store is only asked about eligible replies.
func TestAnIneligibleReplyNeverReachesTheStore(t *testing.T) {
	h, iss, _, _, rc := confSetup(t, "MAKER_CHECKER")
	rec := confPost(h, confToken(iss, session.RoleApprover, confRequester), confPath, confAnswer(confRequestID, "ACCEPT"))
	if code, _ := errBody(t, rec); rec.Code != http.StatusForbidden || code != "APPROVER_NOT_ELIGIBLE" || rc.calls != 0 {
		t.Errorf("%d %s calls %d", rec.Code, rec.Body.String(), rc.calls)
	}
}

// The route appends no event itself: it has no Record port, only the store's Answer. A store that cannot answer without a deadline gets one, and a missing bound is 503.
func TestTheStoreIsCalledWithABoundedContext(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	clk := clock.NewManual(apiEpoch)
	rc := &confAnswers{}
	e := sessionEnv(t, iss)
	e[edge.EnvTenants] = confTenant
	h, err := edge.Load(e.get, os.ReadFile, clk, ids.New(clk, nil), []edge.Route{confirmationRoute(confirmationRouteConfig{Requests: confRequest("MAKER_CHECKER"), Answers: rc, Clock: clk})})
	if err != nil {
		t.Fatal(err)
	}
	rec := confPost(h, confToken(iss, session.RoleApprover, "prn_apr"), confPath, confAnswer(confRequestID, "ACCEPT"))
	if code, _ := errBody(t, rec); rec.Code != http.StatusServiceUnavailable || code != "DEPENDENCY_UNAVAILABLE" || rc.calls != 0 {
		t.Errorf("no timeout configured: %d %s calls %d", rec.Code, rec.Body.String(), rc.calls)
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
	if len(rc.got) != 0 || rq.calls != 1 {
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

// A reply a fraction of a second after the expiry is late: the reply time is rounded up, never down.
func TestASubSecondLateReplyIsExpired(t *testing.T) {
	h, iss, clk, _, rc := confSetup(t, "MAKER_CHECKER")
	clk.Advance(120*1e9 + 5e8)
	rec := confPost(h, confToken(iss, session.RoleApprover, "prn_apr"), confPath, confAnswer(confRequestID, "ACCEPT"))
	if code, _ := errBody(t, rec); rec.Code != http.StatusConflict || code != "CONFIRMATION_EXPIRED" || len(rc.got) != 0 {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
}

// A store that fails or is missing is 503, never a default.
func TestAFailingOrMissingStoreIs503(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	clk := clock.NewManual(apiEpoch)
	auth := confToken(iss, session.RoleApprover, "prn_apr")
	rq := confRequest("MAKER_CHECKER")
	rq.err = errors.New("pgx: down")
	rc := &confAnswers{}
	for name, h := range map[string]http.Handler{
		"lookup fails": confAPI(t, iss, clk, rq, rc),
		"no requests":  confAPI(t, iss, clk, nil, rc),
		"no answers":   confAPI(t, iss, clk, confRequest("MAKER_CHECKER"), nil),
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
	rq, rc := confRequest("MAKER_CHECKER"), &confAnswers{}
	e := sessionEnv(t, iss)
	e[edge.EnvTenants] = confTenant
	cfg := confirmationRouteConfig{Requests: rq, Answers: rc, Clock: clk, Timeout: time.Second,
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

// The stand-in decoder itself (the edge guard already refuses a second document, so this is the decoder's own check): one document, exactly the two members.
func TestTheStandInDecoderIsStrict(t *testing.T) {
	good := confAnswer(confRequestID, "ACCEPT")
	if a, err := decodeConfirmationAnswer([]byte(good)); err != nil || a.RequestID != confRequestID || a.Response != confirm.Accept {
		t.Fatalf("good: %+v %v", a, err)
	}
	for name, body := range map[string]string{"trailing": good + `{}`, "trailing text": good + ` x`, "unknown": `{"request_id":"` + confRequestID + `","response":"ACCEPT","x":1}`, "null": `{"request_id":null,"response":null}`} {
		if _, err := decodeConfirmationAnswer([]byte(body)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// The default decoder is confirmation-answer.v1's generated type (INV-12): a request ID outside the contract's form is refused even when the members are right.
func TestTheDefaultDecoderIsTheContractsType(t *testing.T) {
	for name, body := range map[string]string{"short id": confAnswer("cfr_", "ACCEPT"), "wrong prefix": confAnswer("inv_01ARZ3NDEKTSV4RRFFQ69G5FAV", "ACCEPT"), "enum": confAnswer(confRequestID, "APPROVE")} {
		if _, err := decodeConfirmationAnswer([]byte(body)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// A refused answer leaves one log line of fixed text per outcome, with the outcome, tenant, request ID and the session's principal only (REQ-TRU-009, INV-12; SR item). Never the body,
// the answer or a role. A body-named request ID, which the path does not carry, is never logged.
func TestARefusedAnswerLeavesOneFixedLogLine(t *testing.T) {
	const other = "cfr_01ARZ3NDEKTSV4RRFFQ69G5FAW"
	type row struct {
		name, requirement string
		role              session.Role
		sub, body         string
		advance           time.Duration
		preAnswer         bool
		code              int
		msg, outcome      string
	}
	for _, c := range []row{
		{name: "self-approval", requirement: "MAKER_CHECKER", role: session.RoleApprover, sub: confRequester, body: confAnswer(confRequestID, "ACCEPT"), code: 403, msg: "confirmation answer refused: not eligible", outcome: "not_eligible"},
		{name: "customer on ROLE:approver", requirement: "ROLE:approver", role: session.RoleCustomer, sub: "prn_other", body: confAnswer(confRequestID, "ACCEPT"), code: 403, msg: "confirmation answer refused: not eligible", outcome: "not_eligible"},
		{name: "not yours", requirement: "SELF", role: session.RoleCustomer, sub: "prn_other", body: confAnswer(confRequestID, "ACCEPT"), code: 403, msg: "confirmation answer refused: not eligible", outcome: "not_eligible"},
		{name: "wrong request", requirement: "MAKER_CHECKER", role: session.RoleApprover, sub: "prn_apr", body: confAnswer(other, "ACCEPT"), code: 403, msg: "confirmation answer refused: wrong request", outcome: "wrong_request"},
		{name: "expired", requirement: "MAKER_CHECKER", role: session.RoleApprover, sub: "prn_apr", body: confAnswer(confRequestID, "ACCEPT"), advance: 121 * time.Second, code: 409, msg: "confirmation answer refused: expired", outcome: "expired"},
		{name: "second answer", requirement: "MAKER_CHECKER", role: session.RoleApprover, sub: "prn_apr2", body: confAnswer(confRequestID, "DECLINE"), preAnswer: true, code: 409, msg: "confirmation answer refused: already answered", outcome: "already_answered"},
	} {
		var buf bytes.Buffer
		iss := edgetest.NewIssuer(t)
		clk := clock.NewManual(apiEpoch)
		rq, rc := confRequest(c.requirement), &confAnswers{}
		e := sessionEnv(t, iss)
		e[edge.EnvTenants] = confTenant
		cfg := confirmationRouteConfig{Requests: rq, Answers: rc, Clock: clk, Timeout: time.Second, Logger: slog.New(slog.NewJSONHandler(&buf, nil))}
		h, err := edge.Load(e.get, os.ReadFile, clk, ids.New(clk, nil), []edge.Route{confirmationRoute(cfg)})
		if err != nil {
			t.Fatal(err)
		}
		if c.preAnswer {
			if rec := confPost(h, confToken(iss, session.RoleApprover, "prn_apr"), confPath, confAnswer(confRequestID, "ACCEPT")); rec.Code != http.StatusAccepted {
				t.Fatalf("%s: first %d", c.name, rec.Code)
			}
			if buf.Len() != 0 {
				t.Errorf("%s: an accepted answer logged: %s", c.name, buf.String())
			}
		}
		clk.Advance(c.advance)
		if rec := confPost(h, confToken(iss, c.role, c.sub), confPath, c.body); rec.Code != c.code {
			t.Fatalf("%s: %d %s", c.name, rec.Code, rec.Body.String())
		}
		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		if len(lines) != 1 || lines[0] == "" {
			t.Errorf("%s: want one log line, got %q", c.name, buf.String())
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(lines[0]), &m); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if m["msg"] != c.msg || m["outcome"] != c.outcome || m["tenant"] != confTenant || m["request_id"] != confRequestID || m["principal_id"] != c.sub {
			t.Errorf("%s: %s", c.name, lines[0])
		}
		for _, banned := range []string{"ACCEPT", "DECLINE", "response", "roles", "role", "FAW", "customer", "approver"} {
			if strings.Contains(strings.ReplaceAll(lines[0], "ROLE:approver", ""), banned) {
				t.Errorf("%s: the log line carries %q: %s", c.name, banned, lines[0])
			}
		}
	}
}

// A request the session may not see or that is not found logs nothing here, and a nil logger is fine.
func TestAnAcceptedAnswerAndANilLoggerLogNothing(t *testing.T) {
	h, iss, _, _, rc := confSetup(t, "MAKER_CHECKER")
	if rec := confPost(h, confToken(iss, session.RoleApprover, confRequester), confPath, confAnswer(confRequestID, "ACCEPT")); rec.Code != http.StatusForbidden || len(rc.got) != 0 {
		t.Errorf("nil logger: %d", rec.Code)
	}
}

// A550: the store of confirmation requests is the route's request finder (the request-by-id read), checked at compile time.
var _ ConfirmationRequests = trustpg2.Requests{}
