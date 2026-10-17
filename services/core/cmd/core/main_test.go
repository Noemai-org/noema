package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func get(t *testing.T, check readiness, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	newMux(check).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// REQ-OPS-001, PLT-01: the process is alive.
func TestHealthzIsAlways200(t *testing.T) {
	rec := get(t, notReady, "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("/healthz = %d, want 200", rec.Code)
	}
}

// PLT-01: /readyz is 503 until dependencies exist, and says why.
func TestReadyzIs503WithoutDependencies(t *testing.T) {
	rec := get(t, notReady, "/readyz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not_ready") {
		t.Fatalf("body %q does not say not_ready", rec.Body.String())
	}
}

// PLT-01: once a later task wires dependencies, readiness can turn to 200.
func TestReadyzIs200WhenReady(t *testing.T) {
	rec := get(t, func() (bool, string) { return true, "" }, "/readyz")
	if rec.Code != http.StatusOK {
		t.Fatalf("/readyz = %d, want 200", rec.Code)
	}
}

func TestOnlyGetIsAllowed(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux(notReady).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /healthz = %d, want 405", rec.Code)
	}
}
