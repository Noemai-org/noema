package db_test

// SR's note 1 on noema#234 (and noema#251): the trigger functions of migrations 0001 and 0007 that read no relation still carry no pinned search_path, so a
// session-level search_path or a temporary schema could change what the names inside them mean the day one of them is edited to read a table. They are pinned to
// pg_catalog, pg_temp (ALTER FUNCTION keeps the body). decision_grant_guard is the fourth open one; it reads two tables and is rewritten, qualified, in noema#233
// (migration 0014). REQ-SEC-004, INV-12, ruling A6.

import (
	"strings"
	"testing"
)

func TestTheTriggerFunctionsThatReadNoRelationPinTheirSearchPath(t *testing.T) {
	pool := migrated(t)
	for _, fn := range []string{"reject_mutation", "idempotency_record_guard", "outcome_watch_guard"} {
		var cfg []string
		if err := pool.QueryRow(ctx(t), `SELECT coalesce(p.proconfig, ARRAY[]::text[]) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
			WHERE n.nspname = 'public' AND p.proname = $1`, fn).Scan(&cfg); err != nil {
			t.Fatalf("%s: %v", fn, err)
		}
		pinned := false
		for _, c := range cfg {
			if strings.EqualFold(strings.ReplaceAll(c, `"`, ""), "search_path=pg_catalog, pg_temp") {
				pinned = true
			}
		}
		if !pinned {
			t.Errorf("%s does not pin search_path to pg_catalog, pg_temp (proconfig %v)", fn, cfg)
		}
	}
}
