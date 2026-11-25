package trustport_test

// TRU-11 (REQ-TRU-012, REQ-NFR-002; INV-11, INV-12): the production adapter of intake.Rates. It counts the grants Trust issued to the principal for the capability in the last hour on
// the database clock, as the tenant's own trust_rw login, and compares the count with the descriptor's limit. Real PostgreSQL. SYNTHETIC.

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

type limitsFake map[string]int

func (l limitsFake) RatePerPrincipalPerHour(id string) (int, bool) {
	n, ok := l[id]
	return n, ok && n > 0
}

const (
	rcap  = "fin.cap.create_escalation_case"
	rcap2 = "fin.cap.log_complaint_reference"
)

// grant stores one issued grant of a principal for a capability, made ageSeconds ago by the database clock. The decision is an ALLOW of the same tenant, as the guard requires.
func (w *world) grant(tenant, principal, capability string, ageSeconds int) {
	w.t.Helper()
	n := w.seq
	w.seq++
	dec := fmt.Sprintf("dec_rate%d", n)
	if _, err := w.admin.Exec(tctx(w.t), fmt.Sprintf(`INSERT INTO decision_record (decision_id, tenant_id, json, outcome, intent_hash, policy_versions_json, sig_key_id, sig_value)
		VALUES ('%s', '%s', jsonb_build_object('decision_id', '%[1]s', 'tenant_id', '%[2]s', 'outcome', 'ALLOW', 'sig', jsonb_build_object('key_id', 'k1', 'value', 'aa')) || `+testpg.BoundFieldsSQL+`, 'ALLOW', 'sha256:%064d', '[]', 'k1', 'aa')`,
		dec, tenant, n+1)); err != nil {
		w.t.Fatal(err)
	}
	if _, err := w.admin.Exec(tctx(w.t), fmt.Sprintf(`INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value, created_at)
		VALUES ('grt_rate%d', '%s', '%s', 'n_rate%[1]d', jsonb_build_object('decision_id', '%[3]s', 'tenant_id', '%[2]s', 'principal_id', '%[4]s', 'capability_id', '%[5]s') || `+testpg.GrantBoundSQL+`,
		'pol_1', '1', now() + interval '1 hour', 'k1', 'aa', now() - make_interval(secs => %[6]d))`, n, tenant, dec, principal, capability, ageSeconds)); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) rates(limits trustport.RateLimits) *trustport.Rates {
	w.t.Helper()
	r, err := trustport.NewRates(w.trustPools(), limits, 5*time.Second)
	if err != nil {
		w.t.Fatal(err)
	}
	return r
}

func ask(t *testing.T, r *trustport.Rates, tenant, principal, capability string) bool {
	t.Helper()
	got, err := r.Exceeded(tctx(t), intake.RateQuery{TenantID: tenant, PrincipalID: principal, CapabilityID: capability})
	if err != nil {
		t.Fatalf("Exceeded(%s, %s, %s): %v", tenant, principal, capability, err)
	}
	return got
}

// The rate is used up at the limit: one below it is not, the limit itself is (a limit of 3 allows three writes in the hour and refuses the fourth).
func TestThePrincipalIsOverTheRateAtTheLimitAndNotBelowIt(t *testing.T) {
	w := newWorld(t)
	r := w.rates(limitsFake{rcap: 3})
	for i := 0; i < 2; i++ {
		w.grant(tenantA, "usr_a", rcap, 60)
	}
	if ask(t, r, tenantA, "usr_a", rcap) {
		t.Error("two grants of a limit of three: over the rate")
	}
	w.grant(tenantA, "usr_a", rcap, 60)
	if !ask(t, r, tenantA, "usr_a", rcap) {
		t.Error("three grants of a limit of three: not over the rate")
	}
}

// The hour is counted back from the database clock: a grant of 58 minutes ago counts and one of 62 minutes ago does not.
func TestOnlyTheLastHourIsCountedOnTheDatabaseClock(t *testing.T) {
	w := newWorld(t)
	r := w.rates(limitsFake{rcap: 2})
	w.grant(tenantA, "usr_a", rcap, 62*60)
	w.grant(tenantA, "usr_a", rcap, 90*60)
	w.grant(tenantA, "usr_a", rcap, 58*60)
	if ask(t, r, tenantA, "usr_a", rcap) {
		t.Error("one grant inside the hour of a limit of two: over the rate (the two older grants must not count)")
	}
	w.grant(tenantA, "usr_a", rcap, 10)
	if !ask(t, r, tenantA, "usr_a", rcap) {
		t.Error("two grants inside the hour of a limit of two: not over the rate")
	}
}

// The count is the principal's, for the capability asked about, in the tenant asked about: nobody else's grants move it.
func TestAnotherPrincipalACapabilityOrATenantDoesNotMoveTheCount(t *testing.T) {
	w := newWorld(t)
	r := w.rates(limitsFake{rcap: 1, rcap2: 1})
	w.grant(tenantA, "usr_other", rcap, 30)
	w.grant(tenantA, "usr_a", rcap2, 30)
	w.grant(tenantB, "usr_a", rcap, 30)
	if ask(t, r, tenantA, "usr_a", rcap) {
		t.Error("only other principals', other capabilities' and another tenant's grants exist: over the rate")
	}
	if !ask(t, r, tenantA, "usr_other", rcap) || !ask(t, r, tenantA, "usr_a", rcap2) || !ask(t, r, tenantB, "usr_a", rcap) {
		t.Error("each of the three has one grant of a limit of one and must be over the rate for its own principal, capability and tenant")
	}
}

// A capability that declares no limit is never limited, however many grants exist; nothing is read for it.
func TestACapabilityWithNoDeclaredLimitIsNeverLimited(t *testing.T) {
	w := newWorld(t)
	r := w.rates(limitsFake{rcap: 1})
	for i := 0; i < 5; i++ {
		w.grant(tenantA, "usr_a", rcap2, 30)
	}
	if ask(t, r, tenantA, "usr_a", rcap2) {
		t.Error("a capability with no declared limit is limited")
	}
}

// The adapter refuses what it cannot answer instead of answering "not over the rate": no tenant, no principal, no capability, a tenant with no login.
func TestTheRateAdapterFailsClosed(t *testing.T) {
	w := newWorld(t)
	r := w.rates(limitsFake{rcap: 1})
	for name, q := range map[string]intake.RateQuery{
		"no tenant":     {PrincipalID: "usr_a", CapabilityID: rcap},
		"no principal":  {TenantID: tenantA, CapabilityID: rcap},
		"no capability": {TenantID: tenantA, PrincipalID: "usr_a"},
	} {
		if got, err := r.Exceeded(tctx(t), q); err == nil || got {
			t.Errorf("%s: %v, %v; want false and an error", name, got, err)
		}
	}
	if got, err := r.Exceeded(tctx(t), intake.RateQuery{TenantID: "ten_nobody", PrincipalID: "usr_a", CapabilityID: rcap}); err == nil || got || !errors.Is(err, trustport.ErrNoPool) {
		t.Errorf("a tenant with no login: %v, %v; want false and ErrNoPool", got, err)
	}
	var nilRates *trustport.Rates
	if got, err := nilRates.Exceeded(tctx(t), intake.RateQuery{TenantID: tenantA, PrincipalID: "usr_a", CapabilityID: rcap}); err == nil || got {
		t.Errorf("a nil adapter: %v, %v", got, err)
	}
}

func TestTheRateAdapterNeedsItsPartsAndATimeout(t *testing.T) {
	w := newWorld(t)
	for name, build := range map[string]func() (*trustport.Rates, error){
		"no pools":   func() (*trustport.Rates, error) { return trustport.NewRates(nil, limitsFake{}, time.Second) },
		"no limits":  func() (*trustport.Rates, error) { return trustport.NewRates(w.trustPools(), nil, time.Second) },
		"no timeout": func() (*trustport.Rates, error) { return trustport.NewRates(w.trustPools(), limitsFake{}, 0) },
	} {
		if r, err := build(); !errors.Is(err, trustport.ErrConfig) || r != nil {
			t.Errorf("%s: %v, %v; want ErrConfig and no adapter", name, r, err)
		}
	}
}
