package api_test

// A682: the bench control API delivers a webhook to the relay, signed with mock-case's own SYNTHETIC key. These tests need no database: the two routes touch no store. They run the real
// bench handler on a real listener against a real receiver, and check the delivery with a verifier written from the Standard Webhooks text.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/synthetic/mock_case/internal/api"
	"github.com/Noemai-org/noema/synthetic/mock_case/internal/config"
)

const hookSecret = "whsec_" + "MfKQ9r8GKYqrTwjU" + "PD8ILPZIo2LaLaSw"

type hookRecv struct {
	mu     sync.Mutex
	hdrs   []http.Header
	bodies []string
	status int
}

func (h *hookRecv) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	h.mu.Lock()
	h.hdrs, h.bodies = append(h.hdrs, r.Header.Clone()), append(h.bodies, string(b))
	st := h.status
	h.mu.Unlock()
	if st == 0 {
		st = 200
	}
	w.WriteHeader(st)
}

func hookVerify(h http.Header, body string) bool {
	key, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(hookSecret, "whsec_"))
	ts, err := strconv.ParseInt(h.Get("webhook-timestamp"), 10, 64)
	if err != nil || time.Since(time.Unix(ts, 0)) > 5*time.Minute || time.Until(time.Unix(ts, 0)) > 5*time.Minute {
		return false
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(h.Get("webhook-id") + "." + h.Get("webhook-timestamp") + "." + body))
	return hmac.Equal([]byte(h.Get("webhook-signature")), []byte("v1,"+base64.StdEncoding.EncodeToString(m.Sum(nil))))
}

// hookEnv builds a server with no store; target "" leaves the webhook settings unset.
func hookEnv(t *testing.T, target string) *httptest.Server {
	t.Helper()
	env := map[string]string{"MOCK_CASE_BENCH_BIND_VIA": "mock-case-db", "MOCK_CASE_DB_APP_PASSWORD_FILE": "/app", "MOCK_CASE_DB_ADMIN_PASSWORD_FILE": "/admin", "MOCK_CASE_RUNTIME_TOKEN_FILE": "/rt", "MOCK_CASE_BENCH_TOKEN_FILE": "/bt"}
	files := map[string]string{"/app": "pw1", "/admin": "pw2", "/rt": runtimeToken, "/bt": benchToken, "/wh": hookSecret}
	if target != "" {
		env["MOCK_CASE_WEBHOOK_TARGET_URL"], env["MOCK_CASE_WEBHOOK_SIGNING_SECRET_FILE"] = target, "/wh"
	}
	cfg, err := config.Load(func(k string) string { return env[k] }, func(p string) ([]byte, error) {
		if v, ok := files[p]; ok {
			return []byte(v), nil
		}
		return nil, errors.New("no file")
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(api.New(cfg, nil, slog.New(slog.NewTextHandler(io.Discard, nil))).BenchHandler())
	t.Cleanup(srv.Close)
	return srv
}

func hookPost(t *testing.T, base, path, token, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", base+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestTheBenchControlDeliversAWebhookSignedAndTheRelayGetsTheExactPayload(t *testing.T) {
	rc := &hookRecv{}
	relay := httptest.NewServer(rc)
	defer relay.Close()
	srv := hookEnv(t, relay.URL+"/webhooks")
	payload := `{"type": "case.synthetic",  "id": "evt_1"}`
	st, body := hookPost(t, srv.URL, "/control/v1/webhooks/deliver", benchToken, `{"message_id":"msg_1","payload":`+payload+`}`)
	if st != 200 || !strings.Contains(body, `"relay_status":200`) || !strings.Contains(body, `"accepted":true`) {
		t.Fatalf("%d %s", st, body)
	}
	if len(rc.bodies) != 1 || rc.bodies[0] != payload || rc.hdrs[0].Get("webhook-id") != "msg_1" || !hookVerify(rc.hdrs[0], rc.bodies[0]) {
		t.Fatalf("the relay must get the payload bytes under a signature that verifies: %q %v", rc.bodies, rc.hdrs)
	}
}

func TestAReplayThroughTheControlKeepsTheIdAndTheBody(t *testing.T) {
	rc := &hookRecv{}
	relay := httptest.NewServer(rc)
	defer relay.Close()
	srv := hookEnv(t, relay.URL)
	hookPost(t, srv.URL, "/control/v1/webhooks/deliver", benchToken, `{"message_id":"msg_2","payload":{"a":1}}`)
	if st, b := hookPost(t, srv.URL, "/control/v1/webhooks/msg_2/replay", benchToken, ``); st != 200 {
		t.Fatalf("%d %s", st, b)
	}
	if len(rc.bodies) != 2 || rc.bodies[0] != rc.bodies[1] || rc.hdrs[1].Get("webhook-id") != "msg_2" || !hookVerify(rc.hdrs[1], rc.bodies[1]) {
		t.Fatalf("%q %v", rc.bodies, rc.hdrs)
	}
	if st, _ := hookPost(t, srv.URL, "/control/v1/webhooks/msg_never/replay", benchToken, ``); st != 422 {
		t.Fatalf("a replay of an id never sent: %d, want 422", st)
	}
}

func TestOnlyTheBenchCredentialOpensTheWebhookRoutesAndNothingIsSentWithout(t *testing.T) {
	rc := &hookRecv{}
	relay := httptest.NewServer(rc)
	defer relay.Close()
	srv := hookEnv(t, relay.URL)
	for _, tok := range []string{"", runtimeToken, "x"} {
		if st, _ := hookPost(t, srv.URL, "/control/v1/webhooks/deliver", tok, `{"message_id":"msg_3","payload":{}}`); st != 401 && st != 403 {
			t.Errorf("credential %q: %d, want 401 or 403", tok, st)
		}
	}
	if len(rc.bodies) != 0 {
		t.Fatalf("a refused call sent %d deliveries", len(rc.bodies))
	}
}

func TestWithoutTheSettingsTheRoutesAnswerNotConfiguredAndSendNothing(t *testing.T) {
	srv := hookEnv(t, "")
	for _, p := range []string{"/control/v1/webhooks/deliver", "/control/v1/webhooks/msg_1/replay"} {
		st, b := hookPost(t, srv.URL, p, benchToken, `{"message_id":"msg_1","payload":{}}`)
		if st != 404 || !strings.Contains(b, "WEBHOOK_NOT_CONFIGURED") {
			t.Errorf("%s: %d %s", p, st, b)
		}
	}
}

func TestABadRequestIsRefusedAndNothingIsSent(t *testing.T) {
	rc := &hookRecv{}
	relay := httptest.NewServer(rc)
	defer relay.Close()
	srv := hookEnv(t, relay.URL)
	for name, body := range map[string]string{"unknown field": `{"message_id":"m","payload":{},"extra":1}`, "no payload": `{"message_id":"m"}`, "array payload": `{"message_id":"m","payload":[]}`,
		"bad id": `{"message_id":"a b","payload":{}}`, "not json": `x`} {
		if st, _ := hookPost(t, srv.URL, "/control/v1/webhooks/deliver", benchToken, body); st < 400 || st >= 500 {
			t.Errorf("%s: %d, want a 4xx", name, st)
		}
	}
	if len(rc.bodies) != 0 {
		t.Fatalf("sent %d, want none", len(rc.bodies))
	}
}

func TestARefusalFromTheRelayIsReportedWithItsStatusAndNotAsAcceptance(t *testing.T) {
	rc := &hookRecv{status: 400}
	relay := httptest.NewServer(rc)
	defer relay.Close()
	srv := hookEnv(t, relay.URL)
	st, body := hookPost(t, srv.URL, "/control/v1/webhooks/deliver", benchToken, `{"message_id":"msg_4","payload":{}}`)
	if st != 200 || !strings.Contains(body, `"relay_status":400`) || !strings.Contains(body, `"accepted":false`) {
		t.Fatalf("%d %s", st, body)
	}
}
