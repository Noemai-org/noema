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

// Two spellings are accepted: `pg_catalog, pg_temp` (noema#234, noema#167) and `pg_catalog, public, pg_temp` (EL's DEBT-21 and the pin in noema#168). What matters is
// that pg_temp is named, and last, so a temporary table cannot shadow a real one, and that pg_catalog comes first.
var pinnedForms = []string{"search_path=pg_catalog, pg_temp", "search_path=pg_catalog, public, pg_temp"}

// isPinned proves the EFFECTIVE search_path of a function, not that some entry looks safe (Codex P2 on noema#251). PostgreSQL applies the entries of proconfig in order, so with a
// repeated search_path the last one wins; a pinned function carries exactly one search_path entry, and that entry is one of the accepted forms. A repeated entry is ambiguous and is
// refused, whatever its values (safe then unsafe, unsafe then safe, or the same twice). Another setting beside it (statement_timeout, say) is ignored; a setting whose name only
// ends in search_path (my.search_path) is a different setting and is not the one that matters.
func isPinned(cfg []string) bool {
	var entries []string
	for _, c := range cfg {
		c = strings.ReplaceAll(c, `"`, "")
		if name, _, ok := strings.Cut(c, "="); ok && strings.EqualFold(strings.TrimSpace(name), "search_path") {
			entries = append(entries, c)
		}
	}
	if len(entries) != 1 {
		return false
	}
	for _, f := range pinnedForms {
		if strings.EqualFold(entries[0], f) {
			return true
		}
	}
	return false
}

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
		if !isPinned(cfg) {
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

// A trigger function that is NOT a SECURITY DEFINER runs with the search_path of the session that fired it, and reads other tables by name. With
// TEMPORARY held, that session can shadow a table the function reads. decision_grant_guard (migration 0001, replaced by the grant-void guard of
// noema#94/#233) reads execution_receipt and grant_spend this way: a temporary execution_receipt holding a forged "confirmed refusal" would let
// execution_rw void a spent grant without any signature check (DEBT-01). Every function that backs a trigger therefore pins its search_path to
// `pg_catalog, pg_temp` too. FAILS today; main has the 0001 guard and the other trigger functions unpinned.
func TestEveryTriggerFunctionPinsItsSearchPathToCatalogAndTemp(t *testing.T) {
	pool := migrated(t)
	rows, err := pool.Query(ctx(t), `SELECT p.oid::regprocedure::text, coalesce(p.proconfig::text[], ARRAY[]::text[])
		FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE p.prorettype = 'trigger'::regtype AND n.nspname = 'public'
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
		if !isPinned(cfg) {
			open = append(open, fn)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(all) < 4 {
		t.Fatalf("found only %d trigger functions (%v): the query is looking in the wrong place", len(all), all)
	}
	if len(open) > 0 {
		t.Fatalf("%d of %d trigger functions do not pin search_path to pg_catalog, pg_temp:\n  %s", len(open), len(all), strings.Join(open, "\n  "))
	}
}
