package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpg2"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/confirm"
)

func ccPools(t *testing.T, tenants ...string) map[string]*pgxpool.Pool {
	t.Helper()
	out := map[string]*pgxpool.Pool{}
	for _, tn := range tenants {
		p, err := pgxpool.New(context.Background(), "postgres://synthetic@127.0.0.1:1/db") // lazy: nothing is dialled
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(p.Close)
		out[tn] = p
	}
	return out
}

// REQ-TRU-009, INV-11, INV-12, AGENT-16 (A341, A724): the confirmation route's store is built over each tenant's own Trust login, with a required bound, and a tenant with no login finds nothing.
func TestTheConfirmationRouteIsBuiltOverTheTenantsTrustLoginsWithARequiredBound(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC))
	env := osEnv(envMap{EnvConfirmationTimeout: "1000"})
	cfg, gap, err := buildConfirmations(env, clk, ids.New(clk, nil), ccPools(t, "ten_aa"), &chain.Appender{}, nil)
	if err != nil || cfg == nil || gap != "" {
		t.Fatalf("config %v, gap %q, err %v; want it built", cfg, gap, err)
	}
	if _, found, err := cfg.Requests.Lookup(context.Background(), "ten_zz", "cfr_ABC12"); found || err != nil {
		t.Errorf("a tenant with no login: found %v, err %v; want not found and no error", found, err)
	}
	if _, _, err := buildConfirmations(osEnv(envMap{EnvConfirmationTimeout: "0"}), clk, ids.New(clk, nil), ccPools(t, "ten_aa"), &chain.Appender{}, nil); err == nil {
		t.Error("a zero bound was accepted")
	}
	cfg, gap, err = buildConfirmations(osEnv(envMap{}), clk, ids.New(clk, nil), ccPools(t, "ten_aa"), &chain.Appender{}, nil)
	if err != nil || cfg != nil || gap == "" {
		t.Errorf("no bound: config %v, gap %q, err %v; want a named gap", cfg, gap, err)
	}
}

// The store's own errors map to the route's: an answered request is ErrConfirmationAnswered, an expired one ErrConfirmationExpired.
func TestTheStoresErrorsMapToTheRoutes(t *testing.T) {
	if !errors.Is(mapConfirmationError(trustpg2.ErrAlreadyAnswered), ErrConfirmationAnswered) || !errors.Is(mapConfirmationError(trustpg2.ErrRequestExpired), ErrConfirmationExpired) {
		t.Error("a store error did not map to the route's")
	}
	if err := mapConfirmationError(errors.New("db")); errors.Is(err, ErrConfirmationAnswered) || errors.Is(err, ErrConfirmationExpired) {
		t.Error("an unrelated error was mapped")
	}
	_ = confirm.StoredRequest{}
}
