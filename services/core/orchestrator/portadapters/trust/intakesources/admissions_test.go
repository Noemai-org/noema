package intakesources_test

// Proves REQ-TRU-007 and INV-09 (the admission time of an intent version, which picks the policy version an AT_ADMISSION rule reads), REQ-SEC-004 and INV-11 (the tenant's own
// login, the same answer for absent and for another tenant's), AGENT-13 (no driver text), AGENT-16 (a configured timeout). The admission time is admitted_intent.created_at of
// that version, in whole Unix seconds, dropped and never rounded up, as policystore.Published drops a publication time.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/intakesources"
)

func admissions(t *testing.T, l intakesources.Logins) *intakesources.Admissions {
	t.Helper()
	a, err := intakesources.NewAdmissions(l, readTimeout)
	if err != nil {
		t.Fatalf("NewAdmissions: %v", err)
	}
	return a
}

func TestAdmittedAtIsTheCreationTimeOfThatVersionInWholeSecondsDropped(t *testing.T) {
	w := newWorld(t)
	v1 := time.Date(2026, 11, 10, 12, 0, 0, 900_000_000, time.UTC) // .9: dropped, never rounded up
	v2 := time.Date(2026, 11, 10, 12, 5, 30, 0, time.UTC)
	w.intent(t, tenantA, "int_adm1", 1, node, v1, resolvedSlots)
	w.intent(t, tenantA, "int_adm1", 2, node, v2, resolvedSlots)
	a := admissions(t, w)
	for _, c := range []struct {
		version int
		want    int64
	}{{1, v1.Unix()}, {2, v2.Unix()}} {
		got, err := a.AdmittedAt(ctx(t), tenantA, "int_adm1", c.version)
		if err != nil || got != c.want {
			t.Errorf("version %d: got %d, %v; want %d", c.version, got, err, c.want)
		}
	}
}

func TestAdmittedAtOfAnotherTenantsOrAnAbsentIntentIsOneAnswer(t *testing.T) {
	w := newWorld(t)
	at := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	w.intent(t, tenantB, "int_adm2", 1, node, at, resolvedSlots)
	w.intent(t, tenantA, "int_adm3", 1, node, at, resolvedSlots)
	a := admissions(t, w)
	_, foreign := a.AdmittedAt(ctx(t), tenantA, "int_adm2", 1) // tenant A's login asks for tenant B's intent
	_, absent := a.AdmittedAt(ctx(t), tenantA, "int_nothere", 1)
	_, noVersion := a.AdmittedAt(ctx(t), tenantA, "int_adm3", 2)
	for name, err := range map[string]error{"foreign": foreign, "absent": absent, "no such version": noVersion} {
		if !errors.Is(err, intakesources.ErrNotFound) {
			t.Errorf("%s: got %v, want ErrNotFound", name, err)
		}
	}
	if foreign == nil || absent == nil || foreign.Error() != absent.Error() {
		t.Errorf("a foreign intent and an absent one must read the same: %v / %v", foreign, absent)
	}
	if got, err := a.AdmittedAt(ctx(t), tenantB, "int_adm2", 1); err != nil || got != at.Unix() {
		t.Errorf("the owner reads its own intent: %d, %v", got, err)
	}
}

func TestAdmittedAtRefusesAnAdmissionTimeBeforeOneSecond(t *testing.T) {
	w := newWorld(t)
	w.intent(t, tenantA, "int_adm4", 1, node, time.Unix(0, 0).UTC(), resolvedSlots)
	if got, err := admissions(t, w).AdmittedAt(ctx(t), tenantA, "int_adm4", 1); !errors.Is(err, intakesources.ErrBadAnswer) || got != 0 {
		t.Errorf("an admission at second 0 is no time: got %d, %v", got, err)
	}
}

func TestAdmittedAtRefusesAQueryOutsideItsShapeBeforeAskingAnyLogin(t *testing.T) {
	w := newWorld(t)
	a := admissions(t, w)
	for name, c := range map[string]struct {
		tenant, intent string
		version        int
	}{
		"no tenant": {"", "int_adm1", 1}, "bad tenant": {"tenant_a", "int_adm1", 1}, "no intent": {tenantA, "", 1}, "bad intent": {tenantA, "intent_1", 1},
		"version zero": {tenantA, "int_adm1", 0}, "negative version": {tenantA, "int_adm1", -1}, "version too large": {tenantA, "int_adm1", 1_000_000_000},
		"a reference form": {tenantA, "int_adm1@1", 1},
	} {
		if got, err := a.AdmittedAt(ctx(t), c.tenant, c.intent, c.version); !errors.Is(err, intakesources.ErrQuery) || got != 0 {
			t.Errorf("%s: got %d, %v; want ErrQuery", name, got, err)
		}
	}
	if w.calls != 0 {
		t.Errorf("a login was asked for %d times for queries that are refused", w.calls)
	}
}

func TestAdmittedAtNeedsADeadlineAndAConfiguredReader(t *testing.T) {
	w := newWorld(t)
	if _, err := admissions(t, w).AdmittedAt(context.Background(), tenantA, "int_adm1", 1); !errors.Is(err, intakesources.ErrNoDeadline) {
		t.Errorf("no deadline: %v", err)
	}
	if w.calls != 0 {
		t.Error("a login was asked for without a deadline")
	}
	if _, err := intakesources.NewAdmissions(nil, readTimeout); !errors.Is(err, intakesources.ErrConfig) {
		t.Errorf("no logins: %v", err)
	}
	if _, err := intakesources.NewAdmissions(w, 0); !errors.Is(err, intakesources.ErrConfig) {
		t.Errorf("a timeout of zero: %v", err)
	}
	var none *intakesources.Admissions
	if _, err := none.AdmittedAt(ctx(t), tenantA, "int_adm1", 1); !errors.Is(err, intakesources.ErrConfig) {
		t.Errorf("a nil reader: %v", err)
	}
}

func TestAdmittedAtHasNoLoginForAnUnknownTenantAndNoFallback(t *testing.T) {
	w := newWorld(t)
	w.intent(t, tenantA, "int_adm5", 1, node, time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC), resolvedSlots)
	a := admissions(t, w)
	if _, err := a.AdmittedAt(ctx(t), "ten_cc", "int_adm5", 1); !errors.Is(err, intakesources.ErrStore) {
		t.Errorf("a tenant with no login: %v", err)
	}
	nilLogin := loginFunc(func(string) (intakesources.Querier, error) { return nil, nil })
	if _, err := admissions(t, nilLogin).AdmittedAt(ctx(t), tenantA, "int_adm5", 1); !errors.Is(err, intakesources.ErrStore) {
		t.Errorf("a nil login: %v", err)
	}
}

func TestAdmittedAtHidesTheDriversTextAndKeepsTheCause(t *testing.T) {
	boom := errors.New("connection refused by db-host-17.internal as login trust_rw__ten_aa")
	a := admissions(t, loginFunc(func(string) (intakesources.Querier, error) { return failingLogin{boom}, nil }))
	_, err := a.AdmittedAt(ctx(t), tenantA, "int_adm1", 1)
	if !errors.Is(err, intakesources.ErrStore) || !errors.Is(err, boom) {
		t.Fatalf("got %v; want ErrStore with the cause reachable", err)
	}
	if strings.Contains(err.Error(), "db-host-17") || strings.Contains(err.Error(), "ten_aa") || strings.Contains(err.Error(), "int_adm1") {
		t.Errorf("the text names a server, a login, a tenant or an intent: %q", err.Error())
	}
}

func TestAdmittedAtIsBoundedByTheConfiguredTimeout(t *testing.T) {
	a, err := intakesources.NewAdmissions(loginFunc(func(string) (intakesources.Querier, error) { return blockingLogin{}, nil }), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	start := time.Now()
	_, err = a.AdmittedAt(c, tenantA, "int_adm1", 1)
	if !errors.Is(err, intakesources.ErrStore) || !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 10*time.Second {
		t.Errorf("a read that hangs must end at the timeout: %v after %v", err, time.Since(start))
	}
}
