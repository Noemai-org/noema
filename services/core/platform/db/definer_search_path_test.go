package db_test

// SEC-08 (SR), REQ-SEC-004, INV-12, ruling A6: EVERY SECURITY DEFINER function pins its search_path to `pg_catalog, pg_temp`, so a login
// that holds the PUBLIC TEMPORARY privilege cannot put a temporary table of its own ahead of the table a definer function reads.
// noema#234 closes this for the five functions owned by record_append. This is the catalog-wide check: it names every other definer
// function that is still open. FAILS today for the 0007 functions (outcome_record_verdict, outcome_verdict_complete,
// recovery_request_verdict_guard), whose search_path is `pg_catalog, public`: pg_temp is then searched first for relations.
// A GUARD in the same file keeps the list of definer functions from silently shrinking.

import (
	"sort"
	"strings"
	"testing"
)

const pinned = "search_path=pg_catalog, pg_temp"

func TestEverySecurityDefinerFunctionPinsItsSearchPathToCatalogAndTemp(t *testing.T) {
	pool := migrated(t)
	rows, err := pool.Query(ctx(t), `SELECT p.oid::regprocedure::text, coalesce(p.proconfig::text[], ARRAY[]::text[])
		FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE p.prosecdef AND n.nspname = 'public'
		  AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = p.oid AND d.deptype = 'e')
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var open, all []string
	for rows.Next() {
		var fn string
		var cfg []string
		if err := rows.Scan(&fn, &cfg); err != nil {
			t.Fatal(err)
		}
		all = append(all, fn)
		ok := false
		for _, c := range cfg {
			if strings.EqualFold(strings.ReplaceAll(c, `"`, ""), pinned) {
				ok = true
			}
		}
		if !ok {
			open = append(open, fn)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(all) < 8 {
		t.Fatalf("found only %d SECURITY DEFINER functions (%v): the query is looking in the wrong place", len(all), all)
	}
	sort.Strings(open)
	if len(open) > 0 {
		t.Fatalf("%d of %d SECURITY DEFINER functions do not pin search_path to pg_catalog, pg_temp, so a TEMP table can shadow what they read:\n  %s",
			len(open), len(all), strings.Join(open, "\n  "))
	}
}
