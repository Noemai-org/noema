package main

// REC-06 / REQ-AUD-001, REQ-AUD-005: the chain verifier job that CI runs on every merge and the nightly workflow runs again, as one command (cmd/record-verify).
// These tests need no database: the verifier is a fake that reports what the real one reports, so they prove what the command does with a report: the exit
// code, what it prints and what it never prints (AGENT-13). The real verifier over a real Record is exercised by ci_test.go. Every tenant, ID and key is SYNTHETIC.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/record/verify"
)

// payloadMarker stands for stored event content. No run of the command, on any path, may print it.
const payloadMarker = "SYNTHETIC-PAYLOAD-MARKER-QZX"

// fakeRunner reports what the real Verifier reports: it raises each finding through the alert and the metric it was opened with, then returns the report.
type fakeRunner struct {
	alerts   verify.Alerts
	metrics  verify.Metrics
	findings []verify.Finding
	report   verify.Report
	err      error

	calls       int
	sawDeadline bool
}

func (f *fakeRunner) Run(ctx context.Context) (verify.Report, error) {
	f.calls++
	_, f.sawDeadline = ctx.Deadline()
	var errs []error
	for _, fi := range f.findings {
		f.metrics.ChainBreak(string(fi.Kind))
		if err := f.alerts.ChainBreak(ctx, fi); err != nil {
			errs = append(errs, err)
		}
	}
	return f.report, errors.Join(append(errs, f.err)...)
}

// harness opens a fakeRunner and records how the command used the opener.
type harness struct {
	runner    *fakeRunner
	openErr   error
	opened    int
	closed    int
	openedDSN string
}

func (h *harness) open(_ context.Context, dsn string, a verify.Alerts, m verify.Metrics, _ clock.Clock) (runner, func(), error) {
	h.opened++
	h.openedDSN = dsn
	if h.openErr != nil {
		return nil, nil, h.openErr
	}
	h.runner.alerts, h.runner.metrics = a, m
	return h.runner, func() { h.closed++ }, nil
}

var fixedNow = time.Date(2026, 11, 17, 2, 0, 0, 0, time.UTC)

func execute(t *testing.T, env map[string]string, h *harness) (code int, stdout, stderr string) {
	t.Helper()
	var out, errw bytes.Buffer
	code = run(func(k string) string { return env[k] }, &out, &errw, h.open, clock.NewManual(fixedNow))
	return code, out.String(), errw.String()
}

func withDSN(dsn string) map[string]string { return map[string]string{envDSN: dsn} }

const syntheticDSN = "postgres://record_verify:SYNTHETIC-PASSWORD-7c1@db.synthetic.invalid:5432/noema?sslmode=disable"

func ok(tenant string, events int64) verify.TenantResult {
	return verify.TenantResult{TenantID: tenant, Checked: events, Status: verify.StatusOK}
}

func broken(tenant string, seq int64, kind chain.BreakKind, detail string) (verify.TenantResult, verify.Finding) {
	f := verify.Finding{TenantID: tenant, Seq: seq, Kind: kind, Detail: detail}
	return verify.TenantResult{TenantID: tenant, Status: verify.StatusBreak, Finding: &f}, f
}

// resultLine is the last line the command prints: one JSON object, the result of the run.
type resultLine struct {
	Msg             string         `json:"msg"`
	Result          string         `json:"result"`
	RunAt           string         `json:"run_at"`
	Tenants         int            `json:"tenants"`
	Events          int64          `json:"events"`
	Breaks          int            `json:"breaks"`
	ChainBreakTotal map[string]int `json:"noema_record_chain_break_total"`
}

func lastLine(t *testing.T, stdout string) resultLine {
	t.Helper()
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	var r resultLine
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &r); err != nil {
		t.Fatalf("the last line of stdout is not one JSON object: %q: %v", lines[len(lines)-1], err)
	}
	if r.Msg != "RECORD_VERIFY_RESULT" {
		t.Fatalf("the last line is %q, want the RECORD_VERIFY_RESULT line", r.Msg)
	}
	return r
}

// REQ-AUD-001: a chain that verifies exits 0 and says how much it checked.
func TestACleanReportExitsZeroAndCountsWhatItChecked(t *testing.T) {
	h := &harness{runner: &fakeRunner{report: verify.Report{Results: []verify.TenantResult{ok("ten_aa", 4), ok("ten_bb", 2)}}}}
	code, stdout, stderr := execute(t, withDSN(syntheticDSN), h)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr %q", code, stderr)
	}
	r := lastLine(t, stdout)
	if r.Result != "clean" || r.Tenants != 2 || r.Events != 6 || r.Breaks != 0 {
		t.Fatalf("result = %+v, want clean over 2 tenants and 6 events", r)
	}
	if r.RunAt != fixedNow.Format(time.RFC3339) {
		t.Errorf("run_at = %q, want the clock's time %q", r.RunAt, fixedNow.Format(time.RFC3339))
	}
	if h.runner.calls != 1 || !h.runner.sawDeadline {
		t.Errorf("the verifier ran %d times, deadline seen %v: want once, under a deadline (a context without one is refused)", h.runner.calls, h.runner.sawDeadline)
	}
	if h.openedDSN != syntheticDSN {
		t.Error("the command did not open the database it was given")
	}
}

// REQ-AUD-005: a break exits 1, names the tenant, the sequence number and the kind, and counts it in noema_record_chain_break_total{kind}.
func TestABreakExitsOneAndNamesTenantSeqAndKind(t *testing.T) {
	res, f := broken("ten_aa", 2, chain.BreakHash, "the stored hash is not the hash of the stored event")
	h := &harness{runner: &fakeRunner{findings: []verify.Finding{f}, report: verify.Report{Results: []verify.TenantResult{res, ok("ten_bb", 3)}}}}
	code, stdout, _ := execute(t, withDSN(syntheticDSN), h)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	var breakLines []map[string]any
	for _, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("a stdout line is not JSON: %q", line)
		}
		if m["msg"] == "RECORD_CHAIN_BREAK" {
			breakLines = append(breakLines, m)
		}
	}
	if len(breakLines) != 1 {
		t.Fatalf("%d RECORD_CHAIN_BREAK lines, want exactly one per finding", len(breakLines))
	}
	if b := breakLines[0]; b["tenant_id"] != "ten_aa" || b["seq"] != float64(2) || b["kind"] != "HASH" {
		t.Errorf("break line = %v, want tenant ten_aa, seq 2, kind HASH", b)
	}
	r := lastLine(t, stdout)
	if r.Result != "break" || r.Breaks != 1 || r.ChainBreakTotal["HASH"] != 1 {
		t.Fatalf("result = %+v, want one HASH break", r)
	}
}

// A break is the finding that matters: it is exit 1 even when another tenant could not be read, so a nightly alert is never downgraded to "could not run".
func TestABreakWinsOverAnUnreadableTenant(t *testing.T) {
	res, f := broken("ten_aa", 3, chain.BreakPrevHash, "prev_hash is not the hash of the previous event")
	unreadable := verify.TenantResult{TenantID: "ten_bb", Status: verify.StatusError, Err: errors.New("connection reset")}
	h := &harness{runner: &fakeRunner{findings: []verify.Finding{f}, err: errors.New("verify: read"), report: verify.Report{Results: []verify.TenantResult{res, unreadable}}}}
	if code, _, _ := execute(t, withDSN(syntheticDSN), h); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

// Unknown is unknown: an error is never a pass, even when every result in the report looks clean.
func TestAnErrorWithACleanLookingReportIsNotClean(t *testing.T) {
	h := &harness{runner: &fakeRunner{err: errors.New("verify: could not read"), report: verify.Report{Results: []verify.TenantResult{ok("ten_aa", 4)}}}}
	code, stdout, _ := execute(t, withDSN(syntheticDSN), h)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (not verified)", code)
	}
	if r := lastLine(t, stdout); r.Result != "error" {
		t.Fatalf("result = %q, want error", r.Result)
	}
}

// A run that verified nothing is not a pass: Report.Clean is false for no tenants, and a CI job that checks an empty database proves nothing.
func TestNothingToVerifyIsNotAPass(t *testing.T) {
	h := &harness{runner: &fakeRunner{report: verify.Report{}}}
	code, stdout, _ := execute(t, withDSN(syntheticDSN), h)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if r := lastLine(t, stdout); r.Result != "error" || r.Tenants != 0 {
		t.Fatalf("result = %+v, want an error over no tenants", r)
	}
}

// A tenant that could not be read is ERROR (exit 2), never clean and never a break.
func TestAnUnreadableTenantExitsTwo(t *testing.T) {
	unreadable := verify.TenantResult{TenantID: "ten_bb", Status: verify.StatusError, Err: errors.New("connection reset")}
	h := &harness{runner: &fakeRunner{report: verify.Report{Results: []verify.TenantResult{ok("ten_aa", 4), unreadable}}}}
	if code, _, _ := execute(t, withDSN(syntheticDSN), h); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

// AGENT-13: the command prints identifiers and an enum, never stored content, never an error's text and never the connection string.
func TestNoPayloadErrorTextOrConnectionStringIsEverPrinted(t *testing.T) {
	_, f := broken("ten_aa", 2, chain.BreakPayloadHash, "the stored payload cannot be hashed: "+payloadMarker)
	res, _ := broken("ten_aa", 2, chain.BreakPayloadHash, "x")
	cases := map[string]*harness{
		"a finding whose detail holds content": {runner: &fakeRunner{findings: []verify.Finding{f}, report: verify.Report{Results: []verify.TenantResult{res}}}},
		"a verifier error that holds content":  {runner: &fakeRunner{err: errors.New("verify: read: " + payloadMarker + " " + syntheticDSN), report: verify.Report{Results: []verify.TenantResult{ok("ten_aa", 1)}}}},
		"an open error that holds content":     {openErr: errors.New("connect " + syntheticDSN + " " + payloadMarker)},
		"a tenant error that holds content": {runner: &fakeRunner{report: verify.Report{Results: []verify.TenantResult{
			{TenantID: "ten_aa", Status: verify.StatusError, Err: errors.New(payloadMarker)}}}}},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			_, stdout, stderr := execute(t, withDSN(syntheticDSN), h)
			for _, banned := range []string{payloadMarker, "SYNTHETIC-PASSWORD-7c1", "db.synthetic.invalid", "record_verify:"} {
				if strings.Contains(stdout+stderr, banned) {
					t.Errorf("the output holds %q:\nstdout %q\nstderr %q", banned, stdout, stderr)
				}
			}
		})
	}
}

// The variable is named, its value never is.
func TestAMissingDatabaseURLExitsTwoWithoutOpeningAnything(t *testing.T) {
	for name, env := range map[string]map[string]string{"unset": {}, "empty": {envDSN: ""}, "blank": {envDSN: "   "}} {
		t.Run(name, func(t *testing.T) {
			h := &harness{runner: &fakeRunner{}}
			code, _, stderr := execute(t, env, h)
			if code != 2 || h.opened != 0 {
				t.Fatalf("exit = %d, opened %d times; want 2 and never opened", code, h.opened)
			}
			if !strings.Contains(stderr, envDSN) {
				t.Errorf("stderr %q does not name %s", stderr, envDSN)
			}
		})
	}
}

// OD-06, "restricted write roles": a login that is not the restricted verification login is refused before anything is read, so a run as the administrator or as a
// power cannot pass for the job.
func TestAnUnrestrictedLoginIsRefusedAndNothingRuns(t *testing.T) {
	h := &harness{openErr: errUnrestrictedLogin, runner: &fakeRunner{}}
	code, stdout, stderr := execute(t, withDSN(syntheticDSN), h)
	if code != 2 || h.runner.calls != 0 {
		t.Fatalf("exit = %d, verifier ran %d times; want 2 and never", code, h.runner.calls)
	}
	if !strings.Contains(stderr, "verification login") {
		t.Errorf("stderr %q does not say that the verification login is required", stderr)
	}
	if r := lastLine(t, stdout); r.Result != "error" {
		t.Errorf("result = %q, want error", r.Result)
	}
}

func TestAnOpenFailureExitsTwoAndRunsNothing(t *testing.T) {
	h := &harness{openErr: errors.New("connection refused"), runner: &fakeRunner{}}
	code, _, _ := execute(t, withDSN(syntheticDSN), h)
	if code != 2 || h.runner.calls != 0 {
		t.Fatalf("exit = %d, verifier ran %d times; want 2 and never", code, h.runner.calls)
	}
}

// The database connection is released on every path that opened it.
func TestTheDatabaseIsClosedOnEveryPathThatOpenedIt(t *testing.T) {
	res, f := broken("ten_aa", 2, chain.BreakSeqGap, "expected seq 2")
	cases := map[string]*harness{
		"clean": {runner: &fakeRunner{report: verify.Report{Results: []verify.TenantResult{ok("ten_aa", 1)}}}},
		"break": {runner: &fakeRunner{findings: []verify.Finding{f}, report: verify.Report{Results: []verify.TenantResult{res}}}},
		"error": {runner: &fakeRunner{err: errors.New("x"), report: verify.Report{}}},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			execute(t, withDSN(syntheticDSN), h)
			if h.opened != 1 || h.closed != 1 {
				t.Fatalf("opened %d, closed %d; want 1 and 1", h.opened, h.closed)
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// REQ-AUD-005: "Its failure is returned by the job, never swallowed": an alert line that cannot be written is an error.
func TestAnAlertThatCannotBeWrittenIsAnError(t *testing.T) {
	err := newAlerts(failingWriter{}).ChainBreak(context.Background(), verify.Finding{TenantID: "ten_aa", Seq: 2, Kind: chain.BreakHash})
	if err == nil {
		t.Fatal("an alert that could not be written was reported as delivered")
	}
}

// The metric is an enum-labelled counter (PERD section 44.1).
func TestTheMetricCountsBreaksByKind(t *testing.T) {
	m := newMetrics()
	m.ChainBreak("HASH")
	m.ChainBreak("HASH")
	m.ChainBreak("SEQ_GAP")
	got := m.counts()
	if got["HASH"] != 2 || got["SEQ_GAP"] != 1 || len(got) != 2 {
		t.Fatalf("counts = %v, want HASH 2 and SEQ_GAP 1", got)
	}
}
