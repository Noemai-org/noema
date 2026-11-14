package db_test

// SEC-08 (SR), Codex P2 on noema#251 at 22050df: isPinned returned true as soon as ANY proconfig entry matched a safe form, so a function that also carried a later
// unsafe search_path assignment (PostgreSQL applies proconfig entries in order, so the last one is the effective one) passed the guard that exists to catch exactly that.
// The catalog tests read proconfig as the database holds it; this table pins what isPinned does with it, with no database: a function is pinned only when it carries
// ONE search_path entry and that entry is an accepted form. A repeated search_path (safe then unsafe, unsafe then safe, or the same twice) is ambiguous and is refused.

import "testing"

func TestIsPinnedProvesTheEffectiveSearchPathAndRefusesAnAmbiguousOne(t *testing.T) {
	for name, tc := range map[string]struct {
		cfg  []string
		want bool
	}{
		"catalog then temp":                            {[]string{"search_path=pg_catalog, pg_temp"}, true},
		"catalog, public, temp":                        {[]string{"search_path=pg_catalog, public, pg_temp"}, true},
		"quoted names and another setting beside it":   {[]string{"statement_timeout=5s", `search_path="pg_catalog", "pg_temp"`}, true},
		"upper-case key":                               {[]string{"SEARCH_PATH=pg_catalog, pg_temp"}, true},
		"no config at all":                             {nil, false},
		"another setting only":                         {[]string{"statement_timeout=5s"}, false},
		"safe, then an unsafe one effective later":     {[]string{"search_path=pg_catalog, pg_temp", "search_path=public"}, false},
		"unsafe, then a safe one effective later":      {[]string{"search_path=public", "search_path=pg_catalog, pg_temp"}, false},
		"the same safe setting twice (ambiguous)":      {[]string{"search_path=pg_catalog, pg_temp", "search_path=pg_catalog, pg_temp"}, false},
		"temp first":                                   {[]string{"search_path=pg_temp, pg_catalog"}, false},
		"temp missing":                                 {[]string{"search_path=pg_catalog, public"}, false},
		"temp not last":                                {[]string{"search_path=pg_catalog, pg_temp, public"}, false},
		"catalog not first":                            {[]string{"search_path=public, pg_catalog, pg_temp"}, false},
		"a different setting that ends in search_path": {[]string{"my.search_path=pg_catalog, pg_temp"}, false},
		"trailing text after a safe form":              {[]string{"search_path=pg_catalog, pg_temp; x"}, false},
	} {
		if got := isPinned(tc.cfg); got != tc.want {
			t.Errorf("%s: isPinned(%q) = %v, want %v", name, tc.cfg, got, tc.want)
		}
	}
}
