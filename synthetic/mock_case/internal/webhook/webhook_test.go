package webhook_test

// A682: mock-case delivers a webhook to the relay with its own SYNTHETIC signing key, using the Standard Webhooks v1 scheme that Tushe's response of 7 Oct 2026 (section 4.7) names:
// "HMAC-SHA256 over the raw body plus a timestamp, five-minute tolerance", the same webhook-id on a replay. The scheme is checked against the worked example of the Standard Webhooks
// documentation, computed independently of this code, and against a verifier written here from the scheme's text. Every value is SYNTHETIC (AGENT-25).

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/synthetic/mock_case/internal/webhook"
)

const (
	docSecret    = "whsec_" + "MfKQ9r8GKYqrTwjU" + "PD8ILPZIo2LaLaSw"
	docID        = "msg_p5jXN8AQM9LWM0D4loKWxJek"
	docTimestamp = 1614265330
	docBody      = `{"test": 2432232314}`
	docSignature = "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE="
)

// verify is the scheme as its text gives it, written here and not shared with the code under test.
func verify(secret string, h http.Header, body []byte, now time.Time) bool {
	raw := strings.TrimPrefix(secret, "whsec_")
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return false
	}
	ts, err := strconv.ParseInt(h.Get("webhook-timestamp"), 10, 64)
	if err != nil {
		return false
	}
	if d := now.Sub(time.Unix(ts, 0)); d > 5*time.Minute || d < -5*time.Minute {
		return false
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(h.Get("webhook-id") + "." + h.Get("webhook-timestamp") + "."))
	m.Write(body)
	want := "v1," + base64.StdEncoding.EncodeToString(m.Sum(nil))
	for _, s := range strings.Fields(h.Get("webhook-signature")) {
		if hmac.Equal([]byte(s), []byte(want)) {
			return true
		}
	}
	return false
}

func TestTheSignatureIsTheDocumentedExample(t *testing.T) {
	s, err := webhook.NewSigner(docSecret)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Sign(docID, docTimestamp, []byte(docBody)); got != docSignature {
		t.Fatalf("signature %s, documented %s", got, docSignature)
	}
}

type recv struct {
	mu     sync.Mutex
	hdrs   []http.Header
	bodies [][]byte
	status int
}

func (r *recv) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	b, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	r.hdrs, r.bodies = append(r.hdrs, req.Header.Clone()), append(r.bodies, b)
	st := r.status
	r.mu.Unlock()
	if st == 0 {
		st = 200
	}
	w.WriteHeader(st)
}

func sender(t *testing.T, target string, now time.Time) *webhook.Sender {
	t.Helper()
	sg, err := webhook.NewSigner(docSecret)
	if err != nil {
		t.Fatal(err)
	}
	snd, err := webhook.NewSender(target, sg, webhook.Options{Now: func() time.Time { return now }, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return snd
}

func TestADeliveryCarriesTheThreeHeadersAndTheExactBodyAndVerifies(t *testing.T) {
	rc := &recv{}
	srv := httptest.NewServer(rc)
	defer srv.Close()
	now := time.Unix(docTimestamp, 0)
	res, err := sender(t, srv.URL+"/webhooks", now).Deliver(context.Background(), docID, []byte(docBody))
	if err != nil || res.Status != 200 {
		t.Fatalf("%+v %v", res, err)
	}
	if len(rc.bodies) != 1 || string(rc.bodies[0]) != docBody {
		t.Fatalf("the relay must receive the exact body bytes, got %q", rc.bodies)
	}
	h := rc.hdrs[0]
	if h.Get("webhook-id") != docID || h.Get("webhook-timestamp") != strconv.Itoa(docTimestamp) || h.Get("webhook-signature") != docSignature {
		t.Fatalf("headers %v", h)
	}
	if !verify(docSecret, h, rc.bodies[0], now) {
		t.Fatal("an independent verifier refused the delivery")
	}
	if h.Get("Content-Type") != "application/json" || h.Get("X-Mock-Case-Synthetic") != "true" {
		t.Fatalf("content type and the synthetic marker: %v", h)
	}
}

func TestAReplayKeepsTheIdAndTheBodyAndTakesAFreshTimestampAndSignature(t *testing.T) {
	rc := &recv{}
	srv := httptest.NewServer(rc)
	defer srv.Close()
	clock := time.Unix(docTimestamp, 0)
	var mu sync.Mutex
	sg, _ := webhook.NewSigner(docSecret)
	snd, err := webhook.NewSender(srv.URL, sg, webhook.Options{Now: func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := snd.Deliver(context.Background(), docID, []byte(docBody)); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	clock = clock.Add(90 * time.Second)
	mu.Unlock()
	if _, err := snd.Replay(context.Background(), docID); err != nil {
		t.Fatal(err)
	}
	if len(rc.bodies) != 2 || string(rc.bodies[0]) != string(rc.bodies[1]) {
		t.Fatalf("a replay must send the same body bytes, got %q", rc.bodies)
	}
	if rc.hdrs[1].Get("webhook-id") != docID {
		t.Fatalf("a replay must keep the webhook-id (delta v1.3 section 1)")
	}
	if rc.hdrs[1].Get("webhook-timestamp") == rc.hdrs[0].Get("webhook-timestamp") {
		t.Fatalf("a replay is signed again at the time it is sent")
	}
	if !verify(docSecret, rc.hdrs[1], rc.bodies[1], clock) {
		t.Fatal("the replayed delivery does not verify")
	}
}

func TestAReplayOfAnIdThatWasNeverSentIsAnErrorAndSendsNothing(t *testing.T) {
	rc := &recv{}
	srv := httptest.NewServer(rc)
	defer srv.Close()
	if _, err := sender(t, srv.URL, time.Unix(docTimestamp, 0)).Replay(context.Background(), "msg_never"); err == nil {
		t.Fatal("want an error")
	}
	if len(rc.bodies) != 0 {
		t.Fatal("nothing may be sent")
	}
}

func TestTheSameIdWithAnotherBodyIsRefusedHereAndNotSent(t *testing.T) {
	rc := &recv{}
	srv := httptest.NewServer(rc)
	defer srv.Close()
	snd := sender(t, srv.URL, time.Unix(docTimestamp, 0))
	if _, err := snd.Deliver(context.Background(), "msg_a", []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := snd.Deliver(context.Background(), "msg_a", []byte(`{"a":2}`)); err == nil {
		t.Fatal("an id is the idempotency key: another body under it must be refused")
	}
	if len(rc.bodies) != 1 {
		t.Fatalf("sent %d, want 1", len(rc.bodies))
	}
}

func TestABadIdABadBodyOrAnOversizeBodyIsRefusedAndNothingIsSent(t *testing.T) {
	rc := &recv{}
	srv := httptest.NewServer(rc)
	defer srv.Close()
	snd := sender(t, srv.URL, time.Unix(docTimestamp, 0))
	big := `{"x":"` + strings.Repeat("a", webhook.MaxBodyBytes) + `"}`
	for name, c := range map[string]struct{ id, body string }{
		"empty id": {"", `{}`}, "space in id": {"a b", `{}`}, "dot-dot": {"a/b", `{}`}, "long id": {strings.Repeat("a", 129), `{}`},
		"not json": {"msg_1", `not json`}, "a json array": {"msg_1", `[]`}, "empty body": {"msg_1", ``}, "oversize": {"msg_1", big},
	} {
		if _, err := snd.Deliver(context.Background(), c.id, []byte(c.body)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if len(rc.bodies) != 0 {
		t.Fatalf("sent %d, want none", len(rc.bodies))
	}
}

func TestARefusalOrAnErrorFromTheRelayIsReportedAsItIsAndNeverAsADelivery(t *testing.T) {
	for _, status := range []int{400, 409, 500} {
		rc := &recv{status: status}
		srv := httptest.NewServer(rc)
		res, err := sender(t, srv.URL, time.Unix(docTimestamp, 0)).Deliver(context.Background(), "msg_s"+strconv.Itoa(status), []byte(`{}`))
		srv.Close()
		if err != nil || res.Status != status || res.Accepted {
			t.Errorf("status %d: %+v %v; want the status reported and not accepted", status, res, err)
		}
	}
}

func TestARedirectIsNotFollowedAndATimeoutIsAnErrorNotAnAcceptance(t *testing.T) {
	hit := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer other.Close()
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) }))
	defer redir.Close()
	res, err := sender(t, redir.URL, time.Unix(docTimestamp, 0)).Deliver(context.Background(), "msg_r", []byte(`{}`))
	if err != nil || res.Status != 302 || res.Accepted || hit {
		t.Fatalf("a redirect must not be followed: %+v %v hit=%v", res, err, hit)
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(1500 * time.Millisecond) }))
	defer slow.Close()
	sg, _ := webhook.NewSigner(docSecret)
	snd, _ := webhook.NewSender(slow.URL, sg, webhook.Options{Timeout: 200 * time.Millisecond})
	if res, err := snd.Deliver(context.Background(), "msg_t", []byte(`{}`)); err == nil || res.Accepted {
		t.Fatalf("a timeout is an error and not an acceptance: %+v %v", res, err)
	}
}

func TestTheSecretAndTheTargetAreCheckedAndNeverEchoed(t *testing.T) {
	for _, bad := range []string{"", "tinykey", "whsec_!!!notbase64", "whsec_" + base64.StdEncoding.EncodeToString([]byte("tooshort"))} {
		if _, err := webhook.NewSigner(bad); err == nil {
			t.Errorf("secret %q accepted", bad)
		} else if bad != "" && strings.Contains(err.Error(), bad) {
			t.Errorf("the error repeats the secret: %v", err)
		}
	}
	sg, _ := webhook.NewSigner(docSecret)
	for _, bad := range []string{"", "ftp://x/y", "http://u:p@host/x", "http:///x", "relay:8090/webhooks", "http://host/x#frag"} {
		_, err := webhook.NewSender(bad, sg, webhook.Options{})
		if err == nil {
			t.Errorf("target %q accepted", bad)
		} else if strings.Contains(err.Error(), "p@host") {
			t.Errorf("the error repeats the credential: %v", err)
		}
	}
}

func TestARawSecretOfAtLeast24BytesIsAccepted(t *testing.T) {
	sg, err := webhook.NewSigner(strings.Repeat("k", 24))
	if err != nil || sg.Sign("a", 1, []byte("{}")) == "" {
		t.Fatal(err)
	}
}

func TestTheBodyIsSentByteForByteWithItsWhitespace(t *testing.T) {
	rc := &recv{}
	srv := httptest.NewServer(rc)
	defer srv.Close()
	body := " {\"a\":  1,\n \"b\": \"x\"}\n"
	if _, err := sender(t, srv.URL, time.Unix(docTimestamp, 0)).Deliver(context.Background(), "msg_ws", []byte(body)); err != nil {
		t.Fatal(err)
	}
	if string(rc.bodies[0]) != body || !verify(docSecret, rc.hdrs[0], rc.bodies[0], time.Unix(docTimestamp, 0)) {
		t.Fatalf("the signed bytes must be the sent bytes: %q", rc.bodies[0])
	}
}
