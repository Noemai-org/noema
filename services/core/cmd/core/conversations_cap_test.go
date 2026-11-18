package main

// EDG-03, ruling A457 (interim, relayed, recorded in docs#213, not yet merged): every response of POST /v1/conversations is at most 65,536 bytes (the UI transport limit, A446),
// whatever status the gateway chose; an over-long answer becomes a fixed-text 502 DEPENDENCY_UNAVAILABLE (A471, interim, relayed, recorded in docs#216, not yet merged; the code the gateway already uses for an upstream failure, inputgateway/handler.go:45) with no upstream text. REQ-API-001, REQ-INP-001, INV-02, AGENT-13. Bodies are SYNTHETIC.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

func answering(status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

func runCapped(h http.Handler) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/conversations", strings.NewReader("{}")))
	return rec
}

func padded(n int) string { return strings.Repeat("x", n) }

// jsonOf is a JSON object of exactly n bytes: the secret marker inside it must never leave.
func jsonOf(n int, marker string) string {
	head, tail := `{"correlation_id":"corr_01ARZ3NDEKTSV4RRFFQ69G5FAV","pad":"`+marker, `"}`
	return head + padded(n-len(head)-len(tail)) + tail
}

func assertFixedError(t *testing.T, rec *httptest.ResponseRecorder, marker string) {
	t.Helper()
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status %d, want 502 (A471: an over-long upstream answer is 502, not our own 500)", rec.Code)
	}
	if rec.Body.Len() > maxResponseBytes {
		t.Errorf("body %d bytes exceeds the limit", rec.Body.Len())
	}
	b := rec.Body.String()
	if !strings.Contains(b, `"code":"DEPENDENCY_UNAVAILABLE"`) || strings.Contains(b, "INTERNAL_SAFETY_BLOCK") || !strings.Contains(b, `"retryable":true`) || !strings.Contains(b, `"correlation_id":"corr_`) || strings.Contains(b, marker) || strings.Contains(b, "xxxx") {
		t.Errorf("not the fixed-text error: %.200s", b)
	}
	if cl := rec.Header().Get("Content-Length"); cl != "" {
		t.Errorf("an upstream Content-Length leaked: %s", cl)
	}
}

// (1) A 4xx or 5xx body over the limit becomes the fixed error.
func TestAnOverlongErrorBodyBecomesAFixedTextError(t *testing.T) {
	for _, st := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusServiceUnavailable, http.StatusInternalServerError, http.StatusTeapot} {
		rec := runCapped(startConversation(answering(st, `{"error":"SECRETUPSTREAM`+padded(maxResponseBytes+1)+`"}`)))
		assertFixedError(t, rec, "SECRETUPSTREAM")
	}
}

// (2) Exactly the limit passes byte for byte; one more does not.
func TestAnErrorBodyOfExactlyTheLimitPassesUnchanged(t *testing.T) {
	body := padded(maxResponseBytes)
	rec := runCapped(startConversation(answering(http.StatusBadRequest, body)))
	if rec.Code != http.StatusBadRequest || rec.Body.String() != body {
		t.Errorf("exact limit: %d, %d bytes", rec.Code, rec.Body.Len())
	}
	over := runCapped(startConversation(answering(http.StatusBadRequest, body+"y")))
	assertFixedError(t, over, "SECRETUPSTREAM")
}

// (3) A 200 or 202 body is held to the limit too.
func TestASuccessBodyIsHeldToTheLimitToo(t *testing.T) {
	for _, st := range []int{http.StatusOK, http.StatusAccepted} {
		assertFixedError(t, runCapped(startConversation(answering(st, jsonOf(maxResponseBytes+1, "SECRETUPSTREAM")))), "SECRETUPSTREAM")
		ok := runCapped(startConversation(answering(st, jsonOf(maxResponseBytes, ""))))
		if ok.Code != st || !strings.Contains(ok.Body.String(), "investigation_id") || ok.Body.Len() > maxResponseBytes {
			t.Errorf("status %d at the limit: %d %.120s", st, ok.Code, ok.Body.String())
		}
	}
}

// (4) Property: whatever the gateway answers, no response exceeds the limit, and an over-long one never carries upstream text.
func TestNoResponseEverExceedsTheLimit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		st := rapid.SampledFrom([]int{200, 202, 204, 400, 401, 404, 409, 429, 500, 503}).Draw(t, "status")
		n := rapid.IntRange(0, 3*maxResponseBytes).Draw(t, "size")
		chunk := rapid.IntRange(1, maxResponseBytes+10).Draw(t, "chunk")
		inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(st)
			for left := n; left > 0; {
				k := min(left, chunk)
				_, _ = w.Write([]byte(strings.Repeat("U", k)))
				left -= k
			}
		})
		rec := runCapped(startConversation(inner))
		if rec.Body.Len() > maxResponseBytes {
			t.Fatalf("status %d, upstream %d bytes: response %d bytes", st, n, rec.Body.Len())
		}
		if n > maxResponseBytes && strings.Contains(rec.Body.String(), "UUUU") {
			t.Fatalf("upstream text leaked")
		}
	})
}

// (5) The buffering is bounded: the capture keeps at most limit+1 bytes however much is written, in one write or many.
func TestTheCaptureNeverHoldsMoreThanTheLimitPlusOne(t *testing.T) {
	c := &capture{header: http.Header{}, status: http.StatusOK}
	_, _ = c.Write([]byte(padded(10 * maxResponseBytes)))
	if c.body.Len() > maxResponseBytes+1 {
		t.Errorf("one big write: held %d", c.body.Len())
	}
	for i := 0; i < 100; i++ {
		_, _ = c.Write([]byte(padded(maxResponseBytes)))
	}
	if c.body.Len() > maxResponseBytes+1 {
		t.Errorf("many writes: held %d", c.body.Len())
	}
}

// The error echoes a correlation ID only when it fits the edge's own shape; an unbounded or foreign one becomes corr_NONE.
func TestAnErrorEchoesOnlyABoundedCorrelationID(t *testing.T) {
	for in, want := range map[string]string{
		"corr_01ARZ3NDEKTSV4RRFFQ69G5FAV": "corr_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		"corr_" + padded(70000):           "corr_NONE",
		"corr_" + padded(33):              "corr_NONE",
		"corr_a b":                        "corr_NONE",
		"":                                "corr_NONE",
		"cor_01ARZ3NDEKTSV4RRFFQ69G5FAV":  "corr_NONE",
		"corr_01ARZ3NDEKTSV4RRFFQ69G5FAV\"},\"x\":{\"y\"": "corr_NONE",
	} {
		if got := boundedCorrelation(in); got != want {
			t.Errorf("boundedCorrelation(%.40q) = %.40q, want %q", in, got, want)
		}
	}
}
