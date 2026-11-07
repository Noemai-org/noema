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
