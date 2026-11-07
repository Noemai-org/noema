package verify_test

// REC-05 / REQ-AUD-005: the chain verification job. INV-11. Every event is SYNTHETIC; the tampering is done by the
// database administrator with the immutability trigger disabled, the one way past the INSERT-only roles.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/record/verify"
)

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

type world struct {
	admin *pgxpool.Pool
	ver   *pgxpool.Pool            // the verification login (record_verify), the Verifier's database
	pools map[string]*pgxpool.Pool // one execution_rw login per tenant (DEBT-04)
	app   *chain.Appender
	alert *recordingAlerts
	met   *recordingMetrics
	v     *verify.Verifier
}

type recordingAlerts struct {
	findings []verify.Finding
	fail     error
}

func (a *recordingAlerts) ChainBreak(_ context.Context, f verify.Finding) error {
	a.findings = append(a.findings, f)
	return a.fail
}

type recordingMetrics struct{ kinds []string }

func (m *recordingMetrics) ChainBreak(kind string) { m.kinds = append(m.kinds, kind) }

func newWorld(t *testing.T, tenants ...string) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	for _, id := range tenants {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, $1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	pools := map[string]*pgxpool.Pool{}
	for _, id := range tenants {
		pools[id] = testpg.TenantLogin(t, admin, dsn, "execution_rw", id)
	}
	clk := clock.NewManual(time.Date(2026, 11, 3, 2, 0, 0, 0, time.UTC))
	w := &world{admin: admin, pools: pools, alert: &recordingAlerts{}, met: &recordingMetrics{},
		app: &chain.Appender{Clock: clk, IDs: ids.New(clk, nil)}}
	w.ver = testpg.VerifierLogin(t, admin, dsn)
	w.v = &verify.Verifier{DB: w.ver, Alerts: w.alert, Metrics: w.met, Clock: clk}
	return w
}

func (w *world) appendN(t *testing.T, tenant string, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		tx, err := w.pools[tenant].Begin(ctx(t))
		if err != nil {
			t.Fatal(err)
		}
		_, err = w.app.Append(ctx(t), tx, chain.Event{TenantID: tenant, CorrelationID: "corr_abc123", WrittenBy: "core-execution",
			Actor: "core-execution", EventType: "GRANT_SPENT", ObjectRef: fmt.Sprintf("grt_%04d", i), Payload: map[string]any{"n": i}})
		if err != nil {
			_ = tx.Rollback(ctx(t))
			t.Fatalf("append: %v", err)
		}
		if err := tx.Commit(ctx(t)); err != nil {
			t.Fatal(err)
		}
	}
}

func (w *world) tamper(t *testing.T, sql string) {
	t.Helper()
	conn, err := w.admin.Acquire(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx(t), `SET session_replication_role = replica`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx(t), sql); err != nil {
		t.Fatalf("tamper: %v", err)
	}
}

func (w *world) killed(t *testing.T, tenant string) bool {
	t.Helper()
	var k bool
	if err := w.admin.QueryRow(ctx(t), `SELECT kill_switch FROM tenant WHERE tenant_id = $1`, tenant).Scan(&k); err != nil {
		t.Fatal(err)
	}
	return k
}

// A clean Record gives a clean report: no alert, no metric, no kill switch.
func TestACleanRecordGivesACleanReport(t *testing.T) {
	w := newWorld(t, "ten_aa", "ten_bb")
	w.appendN(t, "ten_aa", 4)
	w.appendN(t, "ten_bb", 2)
	rep, err := w.v.Run(ctx(t))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !rep.Clean() || len(rep.Results) != 2 {
		t.Fatalf("report = %+v, want 2 clean tenants", rep)
	}
	if rep.Results[0].Checked != 4 || rep.Results[1].Checked != 2 {
		t.Errorf("checked = %d and %d, want 4 and 2", rep.Results[0].Checked, rep.Results[1].Checked)
	}
	if len(w.alert.findings)+len(w.met.kinds) != 0 || w.killed(t, "ten_aa") || w.killed(t, "ten_bb") {
		t.Fatal("a clean Record raised an alert, a metric or the kill switch")
	}
}

// REQ-AUD-005: the job SHALL detect 100% of seeded tamperings. A break raises the alert and the metric once, and stops
// that tenant's writes (E6 reads the kill switch); the other tenants are still verified and untouched.
func TestEveryTamperingIsDetectedAlertedAndStopsThatTenantOnly(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		kind string
	}{
		{"an edited hashed body", `UPDATE record_event SET body = jsonb_set(body, '{object_ref}', '"grt_9999"') WHERE tenant_id = 'ten_aa' AND seq = 2`, "COLUMNS"},
		{"an edited stored hash", `UPDATE record_event SET hash = 'sha256:` + strings.Repeat("0", 64) + `' WHERE tenant_id = 'ten_aa' AND seq = 2`, "HASH"},
		{"a broken prior hash", `UPDATE record_event SET prev_hash = 'sha256:` + strings.Repeat("1", 64) + `' WHERE tenant_id = 'ten_aa' AND seq = 3`, "PREV_HASH"},
		{"a deleted event", `DELETE FROM record_event WHERE tenant_id = 'ten_aa' AND seq = 2`, "SEQ_GAP"},
		{"an edited payload", `UPDATE record_event SET payload = '{"n": 999}' WHERE tenant_id = 'ten_aa' AND seq = 2`, "PAYLOAD_HASH"},
		{"the newest events dropped while the head is kept", `DELETE FROM record_event WHERE tenant_id = 'ten_aa' AND seq > 2`, "HEAD_MISMATCH"},
		{"the head row removed", `DELETE FROM record_chain_head WHERE tenant_id = 'ten_aa'`, "HEAD_MISMATCH"},
		{"the head moved back to an earlier event", `UPDATE record_chain_head SET seq = 2, hash = (SELECT hash FROM record_event WHERE tenant_id = 'ten_aa' AND seq = 2) WHERE tenant_id = 'ten_aa'`, "HEAD_MISMATCH"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t, "ten_aa", "ten_bb")
			w.appendN(t, "ten_aa", 4)
			w.appendN(t, "ten_bb", 3)
			w.tamper(t, c.sql)
			rep, err := w.v.Run(ctx(t))
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if rep.Clean() {
				t.Fatal("tampering was not detected")
			}
			a, b := rep.Results[0], rep.Results[1]
			if a.TenantID != "ten_aa" || a.Status != verify.StatusBreak || a.Finding == nil || string(a.Finding.Kind) != c.kind {
				t.Fatalf("ten_aa result = %+v, want a %s break", a, c.kind)
			}
			if b.Status != verify.StatusOK || b.Checked != 3 {
				t.Fatalf("ten_bb result = %+v, want verified and clean", b)
			}
			if len(w.alert.findings) != 1 || w.alert.findings[0].TenantID != "ten_aa" || len(w.met.kinds) != 1 || w.met.kinds[0] != c.kind {
				t.Fatalf("alerts %v, metrics %v, want one of each for ten_aa/%s", w.alert.findings, w.met.kinds, c.kind)
			}
			if !w.killed(t, "ten_aa") || w.killed(t, "ten_bb") {
				t.Fatal("the kill switch must be on for ten_aa and off for ten_bb")
			}
		})
	}
}

// If the alert cannot be delivered, the tenant is already stopped, the failure is returned, and nothing is swallowed.
func TestAnAlertThatCannotBeDeliveredIsAnErrorAndTheTenantIsStillStopped(t *testing.T) {
	w := newWorld(t, "ten_aa")
	w.appendN(t, "ten_aa", 3)
	w.tamper(t, `UPDATE record_event SET hash = 'sha256:`+strings.Repeat("0", 64)+`' WHERE seq = 2`)
	boom := errors.New("pager down")
	w.alert.fail = boom
	_, err := w.v.Run(ctx(t))
	if !errors.Is(err, boom) {
		t.Fatalf("Run = %v, want the alert failure", err)
	}
	if !w.killed(t, "ten_aa") {
		t.Fatal("the tenant was not stopped")
	}
}

// A read that fails is not a break and not a pass: the tenant is reported ERROR, the job returns an error, and nothing
// is stopped on a guess. Unknown is unknown.
func TestAReadThatFailsIsNeitherPassNorBreak(t *testing.T) {
	w := newWorld(t, "ten_aa")
	w.appendN(t, "ten_aa", 2)
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	w.ver.Close() // the verifier's database is gone
	rep, err := w.v.Run(c)
	if err == nil {
		t.Fatalf("Run on a closed database succeeded: %+v", rep)
	}
	if rep.Clean() {
		t.Fatal("a failed read was reported clean")
	}
}

// RunTenant is the on-demand form (REQ-AUD-005).
func TestRunTenantVerifiesOneTenantAndRefusesAnUnknownOne(t *testing.T) {
	w := newWorld(t, "ten_aa", "ten_bb")
	w.appendN(t, "ten_aa", 2)
	w.appendN(t, "ten_bb", 2)
	w.tamper(t, `UPDATE record_event SET hash = 'sha256:`+strings.Repeat("0", 64)+`' WHERE tenant_id = 'ten_bb' AND seq = 1`)
	ok, err := w.v.RunTenant(ctx(t), "ten_aa")
	if err != nil || ok.Status != verify.StatusOK {
		t.Fatalf("ten_aa = %+v, %v; want OK", ok, err)
	}
	bad, err := w.v.RunTenant(ctx(t), "ten_bb")
	if err != nil || bad.Status != verify.StatusBreak {
		t.Fatalf("ten_bb = %+v, %v; want a break", bad, err)
	}
	for _, id := range []string{"", "ten_zz", "TEN_AA", "ten_aa'; --"} {
		if r, err := w.v.RunTenant(ctx(t), id); err == nil {
			t.Errorf("RunTenant(%q) = %+v, want an error", id, r)
		}
	}
}

func TestRunNeedsADeadline(t *testing.T) {
	w := newWorld(t, "ten_aa")
	if _, err := w.v.Run(context.Background()); err == nil {
		t.Fatal("Run without a deadline succeeded")
	}
	if _, err := w.v.RunTenant(context.Background(), "ten_aa"); err == nil {
		t.Fatal("RunTenant without a deadline succeeded")
	}
}

// The job never repairs: after a break the rows are exactly as the tamper left them.
func TestTheJobNeverRepairs(t *testing.T) {
	w := newWorld(t, "ten_aa")
	w.appendN(t, "ten_aa", 3)
	w.tamper(t, `UPDATE record_event SET hash = 'sha256:`+strings.Repeat("0", 64)+`' WHERE seq = 2`)
	var before, after string
	q := `SELECT string_agg(seq::text || hash || prev_hash, ',' ORDER BY seq) || (SELECT seq::text || hash FROM record_chain_head WHERE tenant_id = 'ten_aa') FROM record_event WHERE tenant_id = 'ten_aa'`
	if err := w.admin.QueryRow(ctx(t), q).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := w.v.Run(ctx(t)); err != nil {
		t.Fatal(err)
	}
	if err := w.admin.QueryRow(ctx(t), q).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("the job changed the Record")
	}
}

// unreachableSnapshots lists tenants through the real database but cannot open a snapshot: the database drops out between
// the tenant list and the first read.
type unreachableSnapshots struct{ verify.DB }

func (unreachableSnapshots) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return nil, errors.New("connection reset")
}

// A tenant that cannot be read is ERROR, not OK and not BREAK: no alert, no metric, no kill switch, and the run fails.
func TestATenantThatCannotBeReadIsErrorNotCleanAndNotStopped(t *testing.T) {
	w := newWorld(t, "ten_aa")
	w.appendN(t, "ten_aa", 2)
	w.v.DB = unreachableSnapshots{w.admin}
	rep, err := w.v.Run(ctx(t))
	if err == nil {
		t.Fatal("Run succeeded although no tenant could be read")
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != verify.StatusError || rep.Clean() {
		t.Fatalf("report = %+v, want one ERROR result and not clean", rep)
	}
	if len(w.alert.findings)+len(w.met.kinds) != 0 || w.killed(t, "ten_aa") {
		t.Fatal("an unreadable tenant was alerted or stopped on a guess")
	}
}
