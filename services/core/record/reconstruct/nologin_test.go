package reconstruct_test

// A434 (interim; DEC-2026-11-20-architect-rulings-a358-a453), REQ-AUD-003, REQ-API-002, AGENT-13: a tenant with no Record login is a deployment defect. The handler answers 503
// DEPENDENCY_UNAVAILABLE with the same fixed text as every other failure of the Record, never 401 and never 500, and writes one RECORD_ACCESSED line with that outcome. The reader is
// the production reader's own contract: For gives the tenant's handle or an error.

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

// noLogin is a reader that has no login for the tenant; its error carries what a driver or a pool might, so the test sees that none of it is served.
type noLogin struct{}

func (noLogin) For(string) (reconstruct.TxBeginner, error) {
	return nil, errors.New("no login for ten_synth: postgres://record_read_synth@127.0.0.1/noema")
}

// noHandle gives neither an error nor a handle.
type noHandle struct{}

func (noHandle) For(string) (reconstruct.TxBeginner, error) { return nil, nil }

func TestATenantWithNoRecordLoginIs503DependencyUnavailableAndOneAccessLine(t *testing.T) {
	gen := ids.New(clock.NewManual(time.Unix(0, 0)), zeros{})
	audit := Caller{TenantID: "ten_synth", Principal: "auditor:usr_synth_900", Roles: []string{"audit"}}
	for name, dbs := range map[string]reconstruct.TenantDBs{"For fails": noLogin{}, "For gives no handle": noHandle{}} {
		t.Run(name, func(t *testing.T) {
			var log bytes.Buffer
			rec, out := serve(t, &reconstruct.Handler{DBs: dbs, Auth: fixedAuth{audit}, IDs: gen, Log: slog.New(slog.NewJSONHandler(&log, nil))}, "corr_us06")
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("%d %s; want 503, never 401 and never 500", rec.Code, rec.Body.String())
			}
			if code, retry := errorCode(t, out); code != "DEPENDENCY_UNAVAILABLE" || !retry {
				t.Errorf("code %s, retryable %v", code, retry)
			}
			if msg := out["error"].(map[string]any)["message"]; msg != "The Record is not available." {
				t.Errorf("message %q; want the fixed text of the Record's other dependency failures", msg)
			}
			for _, leak := range []string{"ten_synth", "postgres", "record_read", "127.0.0.1", "corr_us06"} {
				if strings.Contains(rec.Body.String(), leak) {
					t.Errorf("the answer carries %q: %s", leak, rec.Body.String())
				}
			}
			line := log.String()
			if n := strings.Count(line, `"msg":"RECORD_ACCESSED"`); n != 1 {
				t.Fatalf("%d RECORD_ACCESSED lines, want 1: %s", n, line)
			}
			for _, want := range []string{`"outcome":"DEPENDENCY_UNAVAILABLE"`, `"tenant_id":"ten_synth"`, `"principal":"auditor:usr_synth_900"`, `"role":"audit"`, `"correlation_id":"corr_us06"`} {
				if !strings.Contains(line, want) {
					t.Errorf("access log %s lacks %s", line, want)
				}
			}
			if strings.Contains(line, "postgres") || strings.Contains(line, "127.0.0.1") {
				t.Errorf("the access log carries driver text: %s", line)
			}
		})
	}
}
