package trustport_test

// The production adapter of intake.Writes.Enabled (STM-01 follow-up; T3 of PERD 23.3; REQ-TRU-002, REQ-AUD-005, REQ-NFR-002; INV-11, INV-12): the tenant's kill switch, read as
// the tenant's own trust_rw login, and the loaded pack. Real PostgreSQL. SYNTHETIC.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

var _ interface {
	Enabled(context.Context, string) (bool, error)
} = (*trustport.KillSwitch)(nil)

func (w *world) kill(tenant string) {
	w.t.Helper()
	if _, err := w.admin.Exec(tctx(w.t), `UPDATE tenant SET kill_switch = true WHERE tenant_id = $1`, tenant); err != nil {
		w.t.Fatal(err)
	}
}

// Writes are enabled for a tenant whose switch is off and not for one whose switch is on, and each tenant's answer is its own (the other's switch changes nothing).
func TestEnabledIsTheTenantsOwnKillSwitchRead(t *testing.T) {
	w := newWorld(t)
	k := w.killSwitch(w.trustPools())
	for _, tenant := range []string{tenantA, tenantB} {
		if on, err := k.Enabled(tctx(t), tenant); err != nil || !on {
			t.Fatalf("%s with the switch off: %v %v", tenant, on, err)
		}
	}
	w.kill(tenantB)
	if on, err := k.Enabled(tctx(t), tenantB); err != nil || on {
		t.Errorf("B with the switch on: %v %v, want false", on, err)
	}
	if on, err := k.Enabled(tctx(t), tenantA); err != nil || !on {
		t.Errorf("A while only B is stopped: %v %v, want true", on, err)
	}
}

// The adapter binds the tenant itself, whatever login it is given: an administrator's connection sees every tenant's row, and still answers for the tenant asked.
func TestEnabledAsksForTheTenantAskedAndNoOther(t *testing.T) {
	w := newWorld(t)
	w.kill(tenantB)
	k := w.killSwitch(trustport.PoolsOf(map[string]*pgxpool.Pool{tenantA: w.admin, tenantB: w.admin}))
	if on, err := k.Enabled(tctx(t), tenantB); err != nil || on {
		t.Errorf("B: %v %v, want false (B is stopped, A's row is not B's)", on, err)
	}
	if on, err := k.Enabled(tctx(t), tenantA); err != nil || !on {
		t.Errorf("A: %v %v, want true", on, err)
	}
}

// A tenant the login does not see is an error, never "enabled" and never "disabled": the answer is not known.
func TestATenantWithNoVisibleRowIsAnErrorAndNeverEnabled(t *testing.T) {
	w := newWorld(t)
	k := w.killSwitch(trustport.PoolsOf(map[string]*pgxpool.Pool{"ten_zz": w.as("trust_rw", tenantA)}))
	if on, err := k.Enabled(tctx(t), "ten_zz"); !errors.Is(err, trustport.ErrNoTenant) || on {
		t.Errorf("got %v %v, want false and ErrNoTenant", on, err)
	}
	none := w.killSwitch(w.trustPools())
	if on, err := none.Enabled(tctx(t), "ten_cc"); !errors.Is(err, trustport.ErrNoPool) || on {
		t.Errorf("a tenant with no login: got %v %v, want false and ErrNoPool", on, err)
	}
	if on, err := none.Enabled(tctx(t), ""); !errors.Is(err, trustport.ErrSession) || on {
		t.Errorf("no tenant: got %v %v, want false and ErrSession", on, err)
	}
}

// A switch that cannot be read is an error and never "enabled" (T3 then ends UNKNOWN; REQ-NFR-002).
func TestAStoreFailureIsAnErrorAndNeverEnabled(t *testing.T) {
	w := newWorld(t)
	k := w.killSwitch(w.trustPools())
	if _, err := w.admin.Exec(tctx(t), `REVOKE SELECT ON tenant FROM trust_rw`); err != nil {
		t.Fatal(err)
	}
	on, err := k.Enabled(tctx(t), tenantA)
	if on || !errors.Is(err, trustport.ErrStore) || errors.Is(err, trustport.ErrNoTenant) || errors.Is(err, intake.ErrNotFound) {
		t.Errorf("got %v %v, want false and ErrStore", on, err)
	}
}

func TestEnabledRespectsTheContextDeadline(t *testing.T) {
	w := newWorld(t)
	k := w.killSwitch(w.trustPools())
	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	if on, err := k.Enabled(expired, tenantA); on || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expired: got %v %v, want false and DeadlineExceeded", on, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if on, err := k.Enabled(cancelled, tenantA); on || !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: got %v %v, want false and Canceled", on, err)
	}
}

// "The pack loaded" is a condition of the adapter: it is not built without one.
func TestTheKillSwitchIsNotBuiltWithoutAPackOrALoginOrADeadline(t *testing.T) {
	w := newWorld(t)
	good := w.trustPools()
	for name, c := range map[string]struct {
		pools   trustport.TenantPools
		pack    string
		timeout time.Duration
	}{
		"no pools":         {nil, "fin@1.0.0", time.Second},
		"no pack":          {good, "", time.Second},
		"a pack off shape": {good, "fin", time.Second},
		"no timeout":       {good, "fin@1.0.0", 0},
	} {
		if _, err := trustport.NewKillSwitch(c.pools, c.pack, c.timeout); !errors.Is(err, trustport.ErrConfig) {
			t.Errorf("%s: got %v, want ErrConfig", name, err)
		}
	}
}
