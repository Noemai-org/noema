package session_test

// TRU-07, REQ-TRU-009, INV-25, INV-12, AGENT-10, SoT 43.2a: the body of confirmation-answer.v1 is {request_id, response} and nothing else. The approver's principal, roles, channel and time come from
// the authenticated session, never from the body. Two layers hold that, and this test runs both on the real session guard: the guard refuses a body that names an identity (and the handler never runs); the generated type
// refuses every other member, because the schema is closed. Values are SYNTHETIC.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/ids"
)

const confirmationAnswerSchema = "../../../../contracts/schemas/confirmation-answer.v1.json"

// guardedAnswer mounts the real session middleware and the real body guard in front of a handler that decodes the body into the generated type, as the approver route does.
func guardedAnswer(t *testing.T, k keys) (h http.Handler, calls *int, decoded *contracts.ConfirmationAnswerV1, decodeErr *error) {
	t.Helper()
	v, clk := verifier(t, k, epoch)
	idg := ids.New(clk, nil)
	guard, err := session.RefuseBodyIdentity(idg, bodyLimit)
	if err != nil {
		t.Fatal(err)
	}
	n, out, derr := 0, contracts.ConfirmationAnswerV1{}, error(nil)
	h = session.Middleware(v, idg)(guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		out = contracts.ConfirmationAnswerV1{}
		derr = json.NewDecoder(r.Body).Decode(&out)
		w.WriteHeader(http.StatusNoContent)
	})))
	return h, &n, &out, &derr
}

func answerProperties(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(confirmationAnswerSchema)
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(s.Properties))
	for n := range s.Properties {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func postAnswer(h http.Handler, tok, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/confirmations/answer", strings.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// INV-12: no member of the answer is one the guard treats as an identity, so the guard lets a body with each of its members through to the handler. The members are read from the schema, not from this test.
func TestTheGuardLetsEveryMemberOfTheConfirmationAnswerThrough(t *testing.T) {
	props := answerProperties(t)
	if len(props) != 2 || props[0] != "request_id" || props[1] != "response" {
		t.Fatalf("the schema has members %v, want exactly [request_id response]", props)
	}
	k := newKeys(t)
	h, calls, _, _ := guardedAnswer(t, k)
	tok := "Bearer " + mint(t, k.priv, goodHeader(), goodClaims(epoch))
	for _, name := range props {
		before := *calls
		if rec := postAnswer(h, tok, `{"`+name+`":"x"}`); rec.Code != http.StatusNoContent || *calls != before+1 {
			t.Errorf("%s: %d %s, handler calls %d->%d; want the guard to let a body with this member through", name, rec.Code, rec.Body.String(), before, *calls)
		}
	}
}

// INV-12, AGENT-10: a body that names an identity is refused 400 INPUT_INVALID and the handler never runs, so the approver cannot be taken from the body.
func TestTheGuardRefusesAnIdentityInAConfirmationAnswer(t *testing.T) {
	k := newKeys(t)
	h, calls, _, _ := guardedAnswer(t, k)
	tok := "Bearer " + mint(t, k.priv, goodHeader(), goodClaims(epoch))
	for _, name := range []string{"tenant_id", "principal", "principal_id", "role", "roles", "session", "user_id", "actor", "sub"} {
		before := *calls
		body := `{"request_id":"cfr_2Q8","response":"ACCEPT","` + name + `":"x"}`
		rec := postAnswer(h, tok, body)
		if rec.Code != http.StatusBadRequest || envelope(t, rec).Code != contracts.ErrorsV2ErrorCodeINPUTINVALID || *calls != before {
			t.Errorf("%s: %d %s, handler calls %d->%d; want 400 INPUT_INVALID and no handler call", name, rec.Code, rec.Body.String(), before, *calls)
		}
	}
}

// INV-12: the names the guard does not list (an approver id, the approver's roles, a channel, a time) are not let into the answer either: the type is closed, so the handler's decode refuses them.
func TestTheClosedTypeRefusesTheMembersTheGuardDoesNotName(t *testing.T) {
	k := newKeys(t)
	h, calls, _, derr := guardedAnswer(t, k)
	tok := "Bearer " + mint(t, k.priv, goodHeader(), goodClaims(epoch))
	for _, name := range []string{"approver_id", "approver_principal_id", "approver_roles", "channel", "at", "correlation_id", "schema_version", "confirmation_request_id", "note"} {
		before := *calls
		rec := postAnswer(h, tok, `{"request_id":"cfr_2Q8","response":"ACCEPT","`+name+`":"x"}`)
		if rec.Code != http.StatusNoContent || *calls != before+1 {
			t.Fatalf("%s: the guard changed: %d, handler calls %d->%d", name, rec.Code, before, *calls)
		}
		if *derr == nil {
			t.Errorf("%s: the generated type accepted a member the contract does not have", name)
		}
	}
}

// REQ-TRU-009: a well-formed answer passes the guard and decodes into the generated type with both members, for each of the two words; a body with a third word, or without a member, does not decode.
func TestAWellFormedConfirmationAnswerDecodesAndABadOneDoesNot(t *testing.T) {
	k := newKeys(t)
	h, _, got, derr := guardedAnswer(t, k)
	tok := "Bearer " + mint(t, k.priv, goodHeader(), goodClaims(epoch))
	for word, want := range map[string]contracts.EnumsV1ConfirmationResponse{"ACCEPT": contracts.EnumsV1ConfirmationResponseACCEPT, "DECLINE": contracts.EnumsV1ConfirmationResponseDECLINE} {
		if rec := postAnswer(h, tok, `{"request_id":"cfr_2Q8","response":"`+word+`"}`); rec.Code != http.StatusNoContent || *derr != nil {
			t.Fatalf("%s: %d, decode error %v", word, rec.Code, *derr)
		}
		if got.RequestId != "cfr_2Q8" || got.Response != want {
			t.Errorf("%s: decoded %+v", word, *got)
		}
	}
	for name, body := range map[string]string{"a third word": `{"request_id":"cfr_2Q8","response":"APPROVE"}`, "lower case": `{"request_id":"cfr_2Q8","response":"accept"}`,
		"no response": `{"request_id":"cfr_2Q8"}`, "no request_id": `{"response":"ACCEPT"}`, "null response": `{"request_id":"cfr_2Q8","response":null}`} {
		postAnswer(h, tok, body)
		if *derr == nil {
			t.Errorf("%s: decoded without an error", name)
		}
	}
}
