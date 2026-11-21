package db_test

// MEM-01 / Founder decision D10 (7 Nov 2026): operational memory is data, never authority. REQ-MEM-001, INV-12, REQ-SEC-004. Only the orchestrator's tenant login
// may record an observation (memory_observation_record, a SECURITY DEFINER function that takes the tenant from the login); no runtime role has any privilege on the
// table itself, so Trust, Execution, Outcome and Evidence cannot read or write it; rows are append-only; an observation holds typed scalars only (no free text).
// All tenants, logins and observations are SYNTHETIC.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
)

const recordObservation = `SELECT memory_observation_record($1, $2, $3, $4::jsonb, $5, now(), 'ORCHESTRATOR', $6)`

func goodObservation() []any {
	return []any{"mem_01", "ADAPTER_BEHAVIOUR", "fin.cap.create_escalation_case", `{"samples": 12, "within_settle_window": true, "source": "fin.src.case_system"}`, "corr_01J9X3", "BEN-VER-0007"}
}

func TestOnlyTheOrchestratorLoginRecordsAnObservationAndItLandsInItsOwnTenant(t *testing.T) {
	admin, dsn := migratedDSN(t)
	for _, id := range []string{"ten_a", "ten_b"} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, $1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	a := testpg.TenantLogin(t, admin, dsn, "orchestrator_rw", "ten_a")
	if _, err := a.Exec(ctx(t), recordObservation, goodObservation()...); err != nil {
		t.Fatalf("the orchestrator login of ten_a must record an observation: %v", err)
	}
	var tenant, kind string
	if err := admin.QueryRow(ctx(t), `SELECT tenant_id, kind FROM memory_observation WHERE memory_id = 'mem_01'`).Scan(&tenant, &kind); err != nil || tenant != "ten_a" || kind != "ADAPTER_BEHAVIOUR" {
		t.Fatalf("stored tenant %q kind %q err %v; the tenant must be the login's", tenant, kind, err)
	}
	// The same memory_id is not recorded twice.
	_, err := a.Exec(ctx(t), recordObservation, goodObservation()...)
	wantCode(t, err, "23505", "recording the same memory_id twice")

	// Every other power is refused the function, and no login of any power holds a privilege on the table.
	for _, role := range []string{"trust_rw", "execution_rw", "outcome_rw", "evidence_rw", "record_read"} {
		p := testpg.TenantLogin(t, admin, dsn, role, "ten_a")
		args := goodObservation()
		args[0] = "mem_" + role
		_, err := p.Exec(ctx(t), recordObservation, args...)
		wantCode(t, err, denied, role+" calling memory_observation_record")
	}
	for _, role := range []string{"trust_rw", "execution_rw", "outcome_rw", "evidence_rw", "orchestrator_rw", "record_read", "record_append", "record_verify", "record_emitter"} {
		for _, priv := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"} {
			var has bool
			if err := admin.QueryRow(ctx(t), `SELECT has_table_privilege($1, 'memory_observation', $2)`, role, priv).Scan(&has); err != nil {
				t.Fatal(err)
			}
			if has {
				t.Errorf("%s holds %s on memory_observation; operational memory is reached only through memory_observation_record (REQ-MEM-001)", role, priv)
			}
		}
	}
	// The orchestrator login cannot read a row either, not even its own.
	_, err = a.Exec(ctx(t), `SELECT * FROM memory_observation`)
	wantCode(t, err, denied, "the orchestrator login reading memory_observation directly")
}

func TestMemoryObservationsAreAppendOnlyAndHoldTypedScalarsOnly(t *testing.T) {
	admin, dsn := migratedDSN(t)
	if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A')`); err != nil {
		t.Fatal(err)
	}
	a := testpg.TenantLogin(t, admin, dsn, "orchestrator_rw", "ten_a")
	if _, err := a.Exec(ctx(t), recordObservation, goodObservation()...); err != nil {
		t.Fatalf("control: %v", err)
	}
	for _, stmt := range []string{
		`UPDATE memory_observation SET kind = 'FAILURE_PATTERN'`,
		`DELETE FROM memory_observation`,
		`TRUNCATE memory_observation`,
	} {
		_, err := admin.Exec(ctx(t), stmt)
		wantCode(t, err, violation, "the administrator running "+stmt)
	}
	bad := map[string]string{
		"free text":          `{"note": "the adapter was slow on Tuesday"}`,
		"a nested object":    `{"a": {"b": 1}}`,
		"an array":           `{"a": [1, 2]}`,
		"a float":            `{"a": 1.5}`,
		"a negative integer": `{"a": -1}`,
		"an upper-case key":  `{"Samples": 1}`,
		"no members":         `{}`,
		"seventeen members":  seventeenMembers(),
		"a null":             `{"a": null}`,
	}
	i := 0
	for name, obs := range bad {
		i++
		args := goodObservation()
		args[0], args[3] = "mem_bad"+string(rune('a'+i)), obs
		_, err := a.Exec(ctx(t), recordObservation, args...)
		wantCode(t, err, violation, "an observation with "+name)
	}
	for name, mutate := range map[string]func([]any){
		"an unknown kind":      func(a []any) { a[1] = "OPINION" },
		"a subject with space": func(a []any) { a[2] = "a b" },
		"no provenance":        func(a []any) { a[5] = "" },
	} {
		args := goodObservation()
		args[0] = "mem_" + strings.ReplaceAll(name, " ", "_")
		mutate(args)
		_, err := a.Exec(ctx(t), recordObservation, args...)
		wantCode(t, err, violation, name)
	}
}

func seventeenMembers() string {
	parts := make([]string, 17)
	for i := range parts {
		parts[i] = fmt.Sprintf(`"k%d": 1`, i+1)
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// Ruling A6 (row-level security with FORCE) applied to this tenant table, REQ-SEC-004, REQ-SEC-004, INV-12: memory_observation has row-level security enabled and FORCED, so the
// owner (the migration role, and the definer of memory_observation_record) is held to a policy too. The one policy is an INSERT policy for the owner whose check is the
// connecting login's tenant (record_login_tenant('write')); nothing else is allowed for anybody, so no policy exists for SELECT, UPDATE, DELETE or ALL.
func TestMemoryObservationHasForcedRowSecurityWithOnlyAnOwnTenantInsertPolicy(t *testing.T) {
	admin, _ := migratedDSN(t)
	var enabled, forced bool
	if err := admin.QueryRow(ctx(t), `SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = 'public.memory_observation'::regclass`).Scan(&enabled, &forced); err != nil {
		t.Fatal(err)
	}
	if !enabled || !forced {
		t.Fatalf("memory_observation: row security enabled=%v forced=%v, want both true (A6)", enabled, forced)
	}
	rows, err := admin.Query(ctx(t), `SELECT polname, polcmd::text, (SELECT string_agg(rolname, ',' ORDER BY rolname) FROM pg_roles WHERE oid = ANY (polroles)), pg_get_expr(polwithcheck, polrelid), coalesce(pg_get_expr(polqual, polrelid), '')
		FROM pg_policy WHERE polrelid = 'public.memory_observation'::regclass`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var name, cmd, roles, check, using string
		if err := rows.Scan(&name, &cmd, &roles, &check, &using); err != nil {
			t.Fatal(err)
		}
		n++
		if cmd != "a" || roles != "noema_migrator" || !strings.Contains(check, "record_login_tenant") || using != "" {
			t.Errorf("policy %s: cmd=%q roles=%q check=%q using=%q, want an INSERT policy for noema_migrator checking record_login_tenant and nothing else", name, cmd, roles, check, using)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("memory_observation has %d policies, want exactly 1 (an INSERT policy for the owner bound to the login's tenant)", n)
	}
}

// What FORCE buys: the owner role, used directly (not through memory_observation_record, so with no tenant login behind it), cannot insert a row. Without FORCE the owner
// bypasses row security and the insert succeeds.
func TestTheOwnerCannotInsertAnObservationOutsideTheWritePath(t *testing.T) {
	admin, _ := migratedDSN(t)
	if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A')`); err != nil {
		t.Fatal(err)
	}
	conn, err := admin.Acquire(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	tx, err := conn.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	if _, err := tx.Exec(ctx(t), `SET LOCAL ROLE noema_migrator`); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx(t), `INSERT INTO public.memory_observation (memory_id, tenant_id, kind, subject_ref, observation, source_correlation_id, observed_at, recorded_by, provenance_ref)
		VALUES ('mem_direct', 'ten_a', 'ADAPTER_BEHAVIOUR', 'fin.cap.x', '{"samples": 1}'::jsonb, 'corr_01', now(), 'ORCHESTRATOR', 'BEN-VER-0007')`)
	if err == nil {
		t.Fatal("the owner inserted into memory_observation directly: row security is not forced on it (A6)")
	}
}

// C2's finding on noema#272 (the same defect as noema#462): a function created without a REVOKE is executable by every role, and noema#268's
// TestEvidenceVerifyHoldsExactlyTheGrantsD9Names counts every non-trigger function in public that evidence_verify can execute and expects none. Neither function of this
// migration is executable by PUBLIC: the CHECK that calls memory_observation_is_typed runs as the owner inside the definer function, which needs no grant. REQ-SEC-004.
func TestNoFunctionOfTheMemoryMigrationIsExecutableByPublic(t *testing.T) {
	admin, _ := migratedDSN(t)
	for _, fn := range []string{"public.memory_observation_is_typed(jsonb)", "public.memory_observation_record(text, text, text, jsonb, text, timestamptz, text, text)"} {
		var can bool
		if err := admin.QueryRow(ctx(t), `SELECT has_function_privilege('public', $1::regprocedure, 'EXECUTE')`, fn).Scan(&can); err != nil {
			t.Fatalf("%s: %v", fn, err)
		}
		if can {
			t.Errorf("%s is executable by PUBLIC", fn)
		}
	}
	// The CHECK still holds: an observation with free text is refused by the constraint even though PUBLIC cannot call the function.
	var typed bool
	if err := admin.QueryRow(ctx(t), `SELECT public.memory_observation_is_typed('{"samples": 12}'::jsonb)`).Scan(&typed); err != nil || !typed {
		t.Fatalf("the owner cannot call the typed check: %v %v", typed, err)
	}
}
