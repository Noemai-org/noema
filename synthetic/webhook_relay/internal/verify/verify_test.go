package verify

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The worked example of the Standard Webhooks / Svix documentation. The signature was also computed with Python's
// hmac module, independently of this code.
const (
	docSecret    = "whsec_" + "MfKQ9r8GKYqrTwjU" + "PD8ILPZIo2LaLaSw"
	docID        = "msg_p5jXN8AQM9LWM0D4loKWxJek"
	docTimestamp = 1614265330
	docBody      = `{"test": 2432232314}`
	docSignature = "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE="
)

func verifier(t testing.TB) *Verifier {
	t.Helper()
	key, err := ParseSecret(docSecret)
	if err != nil {
		t.Fatal(err)
	}
	return New(key)
}

func headers(id string, ts int64, sig string) http.Header {
	h := http.Header{}
	h.Set("webhook-id", id)
	h.Set("webhook-timestamp", strconv.FormatInt(ts, 10))
	h.Set("webhook-signature", sig)
	return h
}

func at(secs int64) time.Time { return time.Unix(secs, 0) }

func TestTheDocumentedExample(t *testing.T) {
	v := verifier(t)
	if got := v.Sign(docID, docTimestamp, []byte(docBody)); got != docSignature {
		t.Fatalf("signature %s, documented %s", got, docSignature)
	}
	d, err := v.Check(headers(docID, docTimestamp, docSignature), []byte(docBody), at(docTimestamp))
	if err != nil || d.ID != docID || d.Timestamp != docTimestamp {
		t.Fatalf("%+v %v", d, err)
	}
}

func reason(err error) Reason {
	if e, ok := err.(*Error); ok {
		return e.Reason
	}
	return "other: " + Reason(err.Error())
}

func TestRefusals(t *testing.T) {
	v := verifier(t)
	body := []byte(docBody)
	now := at(docTimestamp)
	good := headers(docID, docTimestamp, docSignature)
	mutate := func(f func(h http.Header)) http.Header {
		h := good.Clone()
		f(h)
		return h
	}
	cases := map[string]struct {
		h    http.Header
		body []byte
		now  time.Time
		want Reason
	}{
		"no id":                  {mutate(func(h http.Header) { h.Del("webhook-id") }), body, now, MissingHeader},
		"no timestamp":           {mutate(func(h http.Header) { h.Del("webhook-timestamp") }), body, now, MissingHeader},
		"no signature":           {mutate(func(h http.Header) { h.Del("webhook-signature") }), body, now, MissingHeader},
		"id with a space":        {mutate(func(h http.Header) { h.Set("webhook-id", "msg 1") }), body, now, BadID},
		"id too long":            {mutate(func(h http.Header) { h.Set("webhook-id", strings.Repeat("a", 129)) }), body, now, BadID},
		"timestamp not a number": {mutate(func(h http.Header) { h.Set("webhook-timestamp", "yesterday") }), body, now, BadTimestamp},
		"timestamp padded":       {mutate(func(h http.Header) { h.Set("webhook-timestamp", "01614265330") }), body, now, BadTimestamp},
		"timestamp negative":     {mutate(func(h http.Header) { h.Set("webhook-timestamp", "-5") }), body, now, BadTimestamp},
		"timestamp huge":         {mutate(func(h http.Header) { h.Set("webhook-timestamp", "99999999999999") }), body, now, BadTimestamp},
		"body changed":           {good, []byte(`{"test": 2432232315}`), now, BadSignature},
		"id changed":             {mutate(func(h http.Header) { h.Set("webhook-id", "msg_other") }), body, now, BadSignature},
		"timestamp changed":      {mutate(func(h http.Header) { h.Set("webhook-timestamp", strconv.Itoa(docTimestamp+1)) }), body, now, BadSignature},
		"signature wrong": {mutate(func(h http.Header) {
			h.Set("webhook-signature", "v1,"+base64.StdEncoding.EncodeToString(make([]byte, 32)))
		}), body, now, BadSignature},
		"signature not base64": {mutate(func(h http.Header) { h.Set("webhook-signature", "v1,!!!") }), body, now, BadSignature},
		"signature truncated":  {mutate(func(h http.Header) { h.Set("webhook-signature", docSignature[:20]) }), body, now, BadSignature},
		"other version":        {mutate(func(h http.Header) { h.Set("webhook-signature", "v2"+docSignature[2:]) }), body, now, BadSignature},
		"no version":           {mutate(func(h http.Header) { h.Set("webhook-signature", docSignature[3:]) }), body, now, BadSignature},
		"empty body signature": {good, nil, now, BadSignature},
		"too old":              {good, body, at(docTimestamp + 301), StaleTime},
		"too early":            {good, body, at(docTimestamp - 301), FutureTime},
	}
	for name, tc := range cases {
		_, err := v.Check(tc.h, tc.body, tc.now)
		if err == nil || reason(err) != tc.want {
			t.Errorf("%s: got %v, want %s", name, err, tc.want)
		}
	}
}

// E6: a timestamp more than five minutes off is refused, in either direction; exactly five minutes is accepted.
func TestToleranceBoundary(t *testing.T) {
	v := verifier(t)
	h := headers(docID, docTimestamp, docSignature)
	for _, delta := range []int64{0, 1, 299, 300, -299, -300} {
		if _, err := v.Check(h, []byte(docBody), at(docTimestamp+delta)); err != nil {
			t.Errorf("%d s off: %v", delta, err)
		}
	}
	for _, delta := range []int64{301, 3600, -301, -3600} {
		if _, err := v.Check(h, []byte(docBody), at(docTimestamp+delta)); err == nil {
			t.Errorf("%d s off was accepted", delta)
		}
	}
}

func TestKeyRotationAndOtherVersionsInOneHeader(t *testing.T) {
	v := verifier(t)
	other := New([]byte("another-secret-another-secret-0001"))
	rotation := other.Sign(docID, docTimestamp, []byte(docBody)) + " " + docSignature
	if _, err := v.Check(headers(docID, docTimestamp, rotation), []byte(docBody), at(docTimestamp)); err != nil {
		t.Fatalf("one good signature among several must pass: %v", err)
	}
	mixed := "v1a,xxxx " + docSignature
	if _, err := v.Check(headers(docID, docTimestamp, mixed), []byte(docBody), at(docTimestamp)); err != nil {
		t.Fatalf("an entry of another version is ignored: %v", err)
	}
	onlyOther := other.Sign(docID, docTimestamp, []byte(docBody))
	if _, err := v.Check(headers(docID, docTimestamp, onlyOther), []byte(docBody), at(docTimestamp)); err == nil {
		t.Fatal("a signature made with another secret was accepted")
	}
}

func TestParseSecret(t *testing.T) {
	if k, err := ParseSecret(" " + docSecret + "\n"); err != nil || len(k) != 24 {
		t.Fatalf("%d %v", len(k), err)
	}
	if k, err := ParseSecret("a-raw-secret-of-adequate-length"); err != nil || string(k) != "a-raw-secret-of-adequate-length" {
		t.Fatalf("%q %v", k, err)
	}
	for _, bad := range []string{"", "short", "whsec_!!!", "whsec_" + base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := ParseSecret(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestPullTokenIsDerivedAndDoesNotRevealTheSecret(t *testing.T) {
	k1, _ := ParseSecret(docSecret)
	k2, _ := ParseSecret("whsec_" + base64.StdEncoding.EncodeToString([]byte("a-different-secret-of-24b+")))
	a, b := PullToken(k1), PullToken(k2)
	if len(a) != 64 || a == b || a != PullToken(k1) {
		t.Fatalf("%s %s", a, b)
	}
	if strings.Contains(a, docSecret[6:]) {
		t.Fatal("the token contains the secret")
	}
}
