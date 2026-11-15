package db_test

// REC-05 / D5 (Founder decision D5 of 7 Nov 2026, DEC-2026-11-07-founder-decisions-block-d): the chain verifier may set a tenant's kill switch; only a person clears it. REQ-AUD-005, INV-11, INV-12.
// The verification login holds UPDATE on tenant.kill_switch so that it can stop a tenant after a break (migration 0006); that privilege must not let it
// clear the switch, whether the break was real or the verifier was fooled. Synthetic tenants.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
)

func TestTheVerifierSetsTheKillSwitchButCannotClearIt(t *testing.T) {
	admin, dsn := migratedDSN(t)
	for _, id := range []string{"ten_a", "ten_b"} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, $1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	ver := testpg.VerifierLogin(t, admin, dsn)

	// Setting is allowed, for any tenant: the verifier is all-tenant by design (named exception, SR F1).
	if _, err := ver.Exec(ctx(t), `UPDATE tenant SET kill_switch = true WHERE tenant_id = 'ten_a'`); err != nil {
		t.Fatalf("the verifier must be able to set the kill switch: %v", err)
	}
	// Setting it again is not a clear.
	if _, err := ver.Exec(ctx(t), `UPDATE tenant SET kill_switch = true WHERE tenant_id = 'ten_a'`); err != nil {
		t.Fatalf("setting an already set switch is not a clear: %v", err)
	}
	// Clearing is refused, for the tenant it stopped and in a statement that also sets another.
	for _, stmt := range []string{
		`UPDATE tenant SET kill_switch = false WHERE tenant_id = 'ten_a'`,
		`UPDATE tenant SET kill_switch = (tenant_id = 'ten_b')`,
	} {
		_, err := ver.Exec(ctx(t), stmt)
		wantCode(t, err, denied, "the verifier clearing the kill switch: "+stmt)
	}
	var on bool
	if err := admin.QueryRow(ctx(t), `SELECT kill_switch FROM tenant WHERE tenant_id = 'ten_a'`).Scan(&on); err != nil || !on {
		t.Fatalf("ten_a kill_switch = %v, %v; a refused clear must change nothing", on, err)
	}
	// A person clears it: the administrator (the control plane's ops login, EXE-06, uses this path).
	if _, err := admin.Exec(ctx(t), `UPDATE tenant SET kill_switch = false WHERE tenant_id = 'ten_a'`); err != nil {
		t.Fatalf("a person must be able to clear the kill switch: %v", err)
	}
}

// Review of noema#265 (F1): the guard must not depend on WHICH role tries to clear. Today only record_verify holds UPDATE on
// tenant.kill_switch (migration 0006), so no other runtime login can clear it; but a later grant to any other role, by mistake or
// by a future migration, must not turn the switch back off. The test simulates exactly that grant to each runtime role and requires
// the trigger itself, not the missing privilege, to refuse the clear. Only a superuser (the administrator, the break-glass path
// until the control plane's ops login of EXE-06 exists) clears it. REQ-AUD-005, INV-11, INV-12, ruling D5.
func TestNoRuntimeLoginCanClearTheKillSwitchEvenWithTheGrant(t *testing.T) {
	groups := []string{"trust_rw", "execution_rw", "outcome_rw", "evidence_rw", "orchestrator_rw", "record_read"}
	for _, group := range groups {
		t.Run(group, func(t *testing.T) {
			admin, dsn := migratedDSN(t)
			if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A')`); err != nil {
				t.Fatal(err)
			}
			// The mistaken grant being simulated; without it the role has no privilege at all.
			if _, err := admin.Exec(ctx(t), `GRANT SELECT, UPDATE (kill_switch) ON tenant TO `+group); err != nil {
				t.Fatal(err)
			}
			login := testpg.TenantLogin(t, admin, dsn, group, "ten_a")
			if _, err := admin.Exec(ctx(t), `UPDATE tenant SET kill_switch = true WHERE tenant_id = 'ten_a'`); err != nil {
				t.Fatal(err)
			}
			_, err := login.Exec(ctx(t), `UPDATE tenant SET kill_switch = false WHERE tenant_id = 'ten_a'`)
			var pe *pgconn.PgError
			if !errors.As(err, &pe) || pe.Code != denied || !strings.Contains(pe.Message, "kill switch") {
				t.Fatalf("%s clearing the kill switch: got %v, want the guard's refusal (42501, 'kill switch'), not a missing privilege", group, err)
			}
			var on bool
			if err := admin.QueryRow(ctx(t), `SELECT kill_switch FROM tenant WHERE tenant_id = 'ten_a'`).Scan(&on); err != nil || !on {
				t.Fatalf("kill_switch = %v, %v; a refused clear must change nothing", on, err)
			}
		})
	}
}

// Review of noema#265 (second re-review): the guard lets a superuser clear the switch, as the person's break-glass path until the control plane's ops login
// (EXE-06, ruling A71) exists. The database cannot tell a person from a process, so what can be shown is that no SERVICE can be that superuser: every login
// role the deployment creates for a service is NOSUPERUSER. This is a guard on an existing property (it passes today, so there is no red run); it fails the
// day someone adds a superuser login to the bootstrap, which is the only way a runtime process could clear a person's stop. REQ-AUD-005, INV-11, ruling D5.
func TestNoServiceLoginIsCreatedAsASuperuser(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "deploy", "compose", "postgres", "init.sh"))
	if err != nil {
		t.Fatal(err)
	}
	logins := 0
	for n, line := range strings.Split(string(b), "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "--") || !strings.Contains(l, "CREATE ROLE") || !strings.Contains(l, " LOGIN") || strings.Contains(l, "NOLOGIN") {
			continue
		}
		logins++
		if !strings.Contains(l, "NOSUPERUSER") {
			t.Errorf("init.sh line %d creates a login that is not NOSUPERUSER: %s", n+1, l)
		}
	}
	if logins < 4 {
		t.Fatalf("found %d login roles in init.sh; the scan is looking in the wrong place", logins)
	}
}
