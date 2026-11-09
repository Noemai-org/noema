package synthetic_tushe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
)

// Evidence Layer request (noema#205, from Core Engineer 2): an error read that received a body carries the SHA-256 of those bytes, so the
// Evidence Layer can hash an error read like a good one (REQ-CTX-001, INV-19). It is empty only when no bytes arrived (a timeout).
func TestTheCaseClientsReadErrorsCarryTheHashOfTheirBytes(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{
		"404":           {404, `{"error":{"code":"NOT_FOUND"}}`},
		"503":           {503, `{"error":{"code":"UNAVAILABLE"}}`},
		"429":           {429, `{"error":{"code":"RATE_LIMITED"}}`},
		"400":           {400, `{"error":{"code":"BAD_REQUEST"}}`},
		"401":           {401, `{"error":{"code":"UNAUTHORIZED"}}`},
		"malformed 200": {200, `{"case_id":`},
		"empty body":    {500, ``},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			tok := filepath.Join(t.TempDir(), "t")
			_ = os.WriteFile(tok, []byte("runtime-token-runtime-token-runtime-token-0001"), 0o600)
			c, err := NewCaseClient(srv.URL, tok, nil)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			sum := sha256.Sum256([]byte(tc.body))
			want := "sha256:" + hex.EncodeToString(sum[:])
			_, getErr := c.GetCase(ctx, "ten_aa", "case_1")
			_, findErr := c.FindCases(ctx, adapters.CaseQuery{TenantID: "ten_aa", IdempotencyKey: "k"})
			for op, err := range map[string]error{"GetCase": getErr, "FindCases": findErr} {
				var re *adapters.ReadError
				if !errors.As(err, &re) {
					t.Fatalf("%s: want a ReadError, got %v", op, err)
				}
				if re.RawHash != want {
					t.Errorf("%s: RawHash = %q, want %q", op, re.RawHash, want)
				}
			}
		})
	}
}

func TestATimeoutHasNoBytesAndNoHash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	t.Cleanup(srv.Close)
	tok := filepath.Join(t.TempDir(), "t")
	_ = os.WriteFile(tok, []byte("runtime-token-runtime-token-runtime-token-0001"), 0o600)
	c, err := NewCaseClient(srv.URL, tok, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err = c.GetCase(ctx, "ten_aa", "case_1")
	var re *adapters.ReadError
	if !errors.As(err, &re) || re.Failure != adapters.ReadUnavailable || re.RawHash != "" {
		t.Fatalf("got %+v (%v), want UNAVAILABLE with no hash", re, err)
	}
}
