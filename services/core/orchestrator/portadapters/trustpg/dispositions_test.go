package trustpg_test

// TRU-10 (REQ-TRU-010, REQ-SEC-004; INV-12, INV-30; A539, interim, relayed, not yet committed): the by-correlation read of the ESCALATE disposition decision. Real PostgreSQL, real
// tenant logins, the real Record writer. All data is SYNTHETIC (AGENT-25). The database tests need pgsodium and run in CI only.

import (
	"errors"
	"os"
	"regexp"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpg"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

// escalate records an ESCALATE disposition (no step, no grant) for the tenant and correlation, as the tenant's own Trust login, and returns its decision ID.
func (w *world) escalate(t *testing.T, tenant string, pool *pgxpool.Pool, correlation, id string, h byte) string {
	t.Helper()
	b := basis(t)
	reason, err := decide.Core("NOT_AUTHORIZED")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := decide.Admit(decide.IntentView{Version: 2, MaterialFlags: []string{"X"}}).Unresolved()
	d, err := u.Escalate(reason)
	if err != nil {
		t.Fatal(err)
	}
	if d, err = b.Attach(d); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.writer.Write(ctx(t), tx, recorder.Request{TenantID: tenant, CorrelationID: correlation, IntentID: id, IntentHash: hash(h), PolicyID: "fin.pol", Decision: d, Evaluated: b})
	if err != nil {
		_ = tx.Rollback(ctx(t))
		t.Fatal(err)
	}
	if err := tx.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
	if res.Record.Outcome != "ESCALATE" || len(res.Grants) != 0 {
		t.Fatalf("the disposition: %+v", res)
	}
	return string(res.Record.DecisionId)
}

func dispositions(t *testing.T, w *world) *trustpg.Dispositions {
	t.Helper()
	r, err := trustpg.NewDispositions(w.pools, timeout)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// The recovery re-read: the ESCALATE disposition of the correlation is found by tenant and correlation, and the ALLOW that shares the correlation is not it.
func TestTheDispositionIsFoundByCorrelation(t *testing.T) {
	w := newWorld(t) // an ALLOW of tenant A on corr already exists: it is not a disposition
	want := w.escalate(t, tenantA, w.trustA, corr, intentID, 31)
	got, err := dispositions(t, w).DispositionFor(ctx(t), tenantA, corr)
	if err != nil || got != want {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
}

// A correlation with no ESCALATE (only an ALLOW) and one that does not exist are the typed not-found: never an empty id with no error.
func TestACorrelationWithNoEscalateIsNotFound(t *testing.T) {
	w := newWorld(t)
	r := dispositions(t, w)
	for _, c := range []string{corr, "corr_NOPE99"} {
		got, err := r.DispositionFor(ctx(t), tenantA, c)
		if !errors.Is(err, trustport.ErrNoDisposition) || got != "" {
			t.Errorf("%s: got %q, %v; want ErrNoDisposition", c, got, err)
		}
	}
}

// REQ-SEC-004, INV-12: another tenant's disposition is not found, however the correlation is spelled; the tenant of the read picks the login and the rows.
func TestAnotherTenantsCorrelationIsNotFound(t *testing.T) {
	w := newWorld(t)
	w.escalate(t, tenantA, w.trustA, corr, intentID, 31)
	r := dispositions(t, w)
	if got, err := r.DispositionFor(ctx(t), tenantB, corr); !errors.Is(err, trustport.ErrNoDisposition) || got != "" {
		t.Fatalf("tenant B asks for tenant A's correlation: got %q, %v; want ErrNoDisposition", got, err)
	}
	own := w.escalate(t, tenantB, w.trustB, "corr_BBB222", "int_01J9B2", 32)
	if got, err := r.DispositionFor(ctx(t), tenantB, "corr_BBB222"); err != nil || got != own {
		t.Fatalf("tenant B own: got %q, %v; want %q", got, err, own)
	}
	if got, err := r.DispositionFor(ctx(t), tenantA, "corr_BBB222"); !errors.Is(err, trustport.ErrNoDisposition) || got != "" {
		t.Fatalf("tenant A asks for tenant B's correlation: got %q, %v; want ErrNoDisposition", got, err)
	}
}

// Two ESCALATE decisions on one correlation are ambiguous: no id is guessed (a wrong disposition would link a case to the wrong escalation).
func TestTwoEscalatesOnOneCorrelationAreAmbiguousAndNoneIsReturned(t *testing.T) {
	w := newWorld(t)
	w.escalate(t, tenantA, w.trustA, corr, intentID, 31)
	w.escalate(t, tenantA, w.trustA, corr, "int_01J9ZZ", 32)
	got, err := dispositions(t, w).DispositionFor(ctx(t), tenantA, corr)
	if !errors.Is(err, trustport.ErrBadAnswer) || errors.Is(err, trustport.ErrNoDisposition) || got != "" {
		t.Fatalf("got %q, %v; want ErrBadAnswer and no id", got, err)
	}
}

// A tenant with no login is ErrNoPool, never another tenant's login; an incomplete ask is refused and reads nothing.
func TestADispositionReadFailsClosed(t *testing.T) {
	w := newWorld(t)
	r := dispositions(t, w)
	if _, err := r.DispositionFor(ctx(t), "ten_zz", corr); !errors.Is(err, trustpg.ErrNoPool) {
		t.Errorf("a tenant with no login: %v, want ErrNoPool", err)
	}
	for name, a := range map[string][2]string{"no tenant": {"", corr}, "no correlation": {tenantA, ""}} {
		if got, err := r.DispositionFor(ctx(t), a[0], a[1]); err == nil || errors.Is(err, trustport.ErrNoDisposition) || got != "" {
			t.Errorf("%s: got %q, %v; want a refusal that is not a not-found", name, got, err)
		}
	}
	if _, err := (*trustpg.Dispositions)(nil).DispositionFor(ctx(t), tenantA, corr); !errors.Is(err, trustpg.ErrConfig) {
		t.Errorf("a nil reader: %v, want ErrConfig", err)
	}
	if _, err := trustpg.NewDispositions(trustpg.Pools{}, timeout); !errors.Is(err, trustpg.ErrConfig) {
		t.Errorf("no pool: %v, want ErrConfig", err)
	}
}

// The reader writes nothing (REQ-SEC-004; the package's own rule): no SQL literal of dispositions.go writes, and one of them reads decision_record. Source-level check; needs no
// database.
func TestTheDispositionReaderSourceWritesNothing(t *testing.T) {
	src, err := os.ReadFile("dispositions.go")
	if err != nil {
		t.Fatal(err)
	}
	literals := regexp.MustCompile("(?s)`[^`]*`").FindAllString(string(src), -1)
	reads := false
	for _, l := range literals {
		if regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|TRUNCATE|ALTER|DROP|CREATE)\b`).MatchString(l) {
			t.Errorf("an SQL literal of dispositions.go writes: %s", l)
		}
		if regexp.MustCompile(`(?i)\bFROM\s+decision_record\b`).MatchString(l) {
			reads = true
		}
	}
	if !reads {
		t.Fatal("dispositions.go has no SQL that reads decision_record: the reader must read the Record")
	}
}
