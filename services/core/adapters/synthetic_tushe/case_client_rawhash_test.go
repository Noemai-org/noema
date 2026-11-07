package synthetic_tushe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
)

// Evidence Layer request (noema#205): the case client's read errors also carry the hash of the bytes they received (non-2xx bodies and
// malformed 2xx bodies), so an error read can be hashed like a good one.
func TestTheCaseClientsReadErrorsCarryTheHashOfTheirBytes(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{"404": {404, `{"error":{"code":"NOT_FOUND"}}`}, "503": {503, `{"error":{"code":"UNAVAILABLE"}}`}, "malformed 200": {200, `{"case_id":`}, "400": {400, `{"error":{"code":"BAD_REQUEST"}}`}}
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
			_, getErr := c.GetCase(ctx, "ten_aa", "case_1")
			_, findErr := c.FindCases(ctx, adapters.CaseQuery{TenantID: "ten_aa", IdempotencyKey: "k"})
			for op, err := range map[string]error{"GetCase": getErr, "FindCases": findErr} {
				re := sFail(t, err)
				if re.RawHash != sha(tc.body) {
					t.Errorf("%s: RawHash = %q, want %q", op, re.RawHash, sha(tc.body))
				}
			}
		})
	}
}
