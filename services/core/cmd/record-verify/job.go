package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/verify"
)

// envDSN names the variable that holds the connection string of the verification login. The value is a credential: it is read, used and never printed.
const envDSN = "NOEMA_RECORD_VERIFY_DATABASE_URL"

// The exit codes. A break is the finding that matters, so it wins over anything that could not be read in the same run.
const (
	exitClean       = 0 // every tenant verified, and there was at least one
	exitBreak       = 1 // a chain break was found, alerted and counted
	exitNotVerified = 2 // the job could not verify: no URL, a refused login, an unreadable tenant or an error; unknown is never a pass
)

// jobTimeout bounds the whole run: the verifier refuses a context without a deadline. It is an operating limit of this command, not a safety threshold.
const jobTimeout = 10 * time.Minute

// errUnrestrictedLogin is the refusal of a login that is not the restricted verification login (OD-06, restricted write roles).
var errUnrestrictedLogin = errors.New("record-verify: the login is not the restricted verification login")

// runner is the verifier as the command uses it.
type runner interface {
	Run(ctx context.Context) (verify.Report, error)
}

// opener connects, checks the login and returns the verifier with the alert and the metric it must raise, and the function that releases the connection.
type opener func(ctx context.Context, dsn string, a verify.Alerts, m verify.Metrics, clk clock.Clock) (runner, func(), error)

// breakDoc is the alert: identifiers and an enum, never the finding's description (which can quote a stored value) and never event content (AGENT-13).
type breakDoc struct {
	Msg      string `json:"msg"`
	TenantID string `json:"tenant_id"`
	Seq      int64  `json:"seq"`
	Kind     string `json:"kind"`
}

// resultDoc is the last line the command prints.
type resultDoc struct {
	Msg             string         `json:"msg"`
	Result          string         `json:"result"`
	RunAt           string         `json:"run_at"`
	Tenants         int            `json:"tenants"`
	Events          int64          `json:"events"`
	Breaks          int            `json:"breaks"`
	ChainBreakTotal map[string]int `json:"noema_record_chain_break_total"`
}

// alerts writes one RECORD_CHAIN_BREAK line per finding. A line that cannot be written is an error the verifier returns, never swallowed (REQ-AUD-005).
type alerts struct{ w io.Writer }

func newAlerts(w io.Writer) *alerts { return &alerts{w: w} }

func (a *alerts) ChainBreak(_ context.Context, f verify.Finding) error {
	return writeJSON(a.w, breakDoc{Msg: "RECORD_CHAIN_BREAK", TenantID: f.TenantID, Seq: f.Seq, Kind: string(f.Kind)})
}

// metrics counts noema_record_chain_break_total{kind} (PERD section 44.1; the label is an enum).
type metrics struct{ n map[string]int }

func newMetrics() *metrics { return &metrics{n: map[string]int{}} }

func (m *metrics) ChainBreak(kind string) { m.n[kind]++ }

func (m *metrics) counts() map[string]int { return m.n }

func writeJSON(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

// run is the command. It prints identifiers, counts and enums to out and fixed sentences to errw: never an error's text, a finding's description, event content or the
// connection string. It returns the exit code.
func run(env func(string) string, out, errw io.Writer, open opener, clk clock.Clock) int {
	rep, code := verifyRecord(env, errw, out, open, clk)
	result := "clean"
	switch code {
	case exitBreak:
		result = "break"
	case exitNotVerified:
		result = "error"
	}
	line := resultDoc{Msg: "RECORD_VERIFY_RESULT", Result: result, RunAt: clk.Now().Format(time.RFC3339), Tenants: len(rep.report.Results),
		Breaks: rep.breaks, ChainBreakTotal: rep.m.counts()}
	for _, r := range rep.report.Results {
		line.Events += r.Checked
	}
	if err := writeJSON(out, line); err != nil && code == exitClean {
		_, _ = fmt.Fprintln(errw, "record-verify: the result could not be written")
		return exitNotVerified
	}
	return code
}

type jobOutcome struct {
	report verify.Report
	breaks int
	m      *metrics
}

func verifyRecord(env func(string) string, errw, out io.Writer, open opener, clk clock.Clock) (jobOutcome, int) {
	o := jobOutcome{m: newMetrics()}
	dsn := strings.TrimSpace(env(envDSN))
	if dsn == "" {
		_, _ = fmt.Fprintf(errw, "record-verify: %s is not set\n", envDSN)
		return o, exitNotVerified
	}
	ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
	defer cancel()
	r, closeDB, err := open(ctx, dsn, newAlerts(out), o.m, clk)
	if err != nil {
		_, _ = fmt.Fprintln(errw, openFailure(err))
		return o, exitNotVerified
	}
	defer closeDB()
	rep, err := r.Run(ctx)
	o.report = rep
	for _, t := range rep.Results {
		if t.Status == verify.StatusBreak {
			o.breaks++
		}
	}
	switch {
	case o.breaks > 0:
		return o, exitBreak
	case err != nil:
		_, _ = fmt.Fprintln(errw, "record-verify: the verification did not complete"+sqlState(err))
		return o, exitNotVerified
	case !rep.Clean():
		_, _ = fmt.Fprintln(errw, "record-verify: nothing was verified, or a tenant could not be read")
		return o, exitNotVerified
	}
	return o, exitClean
}

func openFailure(err error) string {
	if errors.Is(err, errUnrestrictedLogin) {
		return "record-verify: refused: the job runs only as the verification login (a member of record_verify, not a superuser, with no write privilege on the Record)"
	}
	return "record-verify: could not open the verification session" + sqlState(err)
}

// sqlState adds the five-character SQLSTATE of a database error, the one part of it that is a fixed code.
func sqlState(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && len(pg.Code) == 5 {
		return " (SQLSTATE " + pg.Code + ")"
	}
	return ""
}

// preflight refuses any login but the restricted verification login: not a superuser, a member of record_verify, and no INSERT, UPDATE, DELETE or TRUNCATE on the Record tables
// (OD-06). The run as the administrator or as a power's login would pass the chain check and prove nothing about the roles.
func preflight(ctx context.Context, pool *pgxpool.Pool) error {
	var super, member, writes bool
	err := pool.QueryRow(ctx, `SELECT r.rolsuper, pg_has_role(current_user, 'record_verify', 'MEMBER'),
		has_table_privilege(current_user, 'record_event', 'INSERT,UPDATE,DELETE,TRUNCATE') OR has_table_privilege(current_user, 'record_chain_head', 'INSERT,UPDATE,DELETE,TRUNCATE')
		FROM pg_roles r WHERE r.rolname = current_user`).Scan(&super, &member, &writes)
	if err != nil {
		return fmt.Errorf("record-verify: read the login's privileges: %w", err)
	}
	if super || !member || writes {
		return errUnrestrictedLogin
	}
	return nil
}

// openDatabase is the opener of the command: one pool, the login check, and the REC-05 verifier over it.
func openDatabase(ctx context.Context, dsn string, a verify.Alerts, m verify.Metrics, clk clock.Clock) (runner, func(), error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, nil, errors.New("record-verify: the connection string is not valid")
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, nil, errors.New("record-verify: the connection could not be set up")
	}
	if err := preflight(ctx, pool); err != nil {
		pool.Close()
		return nil, nil, err
	}
	return &verify.Verifier{DB: pool, Alerts: a, Metrics: m, Clock: clk}, pool.Close, nil
}
