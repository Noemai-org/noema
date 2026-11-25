package db_test

// TRU-11 (REQ-TRU-012; migration 0041): the rate check counts the grants of one principal for one capability in the last hour, so the query path has its own named index (AGENTS
// section 7: an explicit index for every query path). It is an index and nothing else: the grant table's guard, grants and policies are untouched. SYNTHETIC.

import (
	"strings"
	"testing"
)

func TestTheGrantRateQueryHasItsNamedIndex(t *testing.T) {
	pool := migrated(t)
	var def string
	err := pool.QueryRow(ctx(t), `SELECT indexdef FROM pg_indexes WHERE schemaname = current_schema() AND tablename = 'decision_grant' AND indexname = 'decision_grant_principal_capability_idx'`).Scan(&def)
	if err != nil {
		t.Fatalf("decision_grant_principal_capability_idx is not there: %v", err)
	}
	for _, want := range []string{"tenant_id", "principal_id", "capability_id", "created_at"} {
		if !strings.Contains(def, want) {
			t.Errorf("the index %q does not cover %s", def, want)
		}
	}
	if strings.Contains(def, "UNIQUE") {
		t.Errorf("the index %q is unique: a principal may hold many grants of one capability", def)
	}
}
