# Status · BEN-03 reader (Benchmark Owner agent) · the harness's read-only reader of Tushe's transitions and event feed

# Session 2026-11-19 · BEN-03 · the reader, built against a local synthetic stand-in

## Session summary
- What changed: `benchmark/oracle_client/tushe_readonly.py` (`TusheReadOnly`: `transitions`, `read_feed`, `events_for_trace`) and 26 tests in `test_tushe_readonly.py` against a loopback stand-in that speaks delta v1.3's two read routes with made-up rows.
- Why: AT-05, AT-14 and AT-17 are shown from Noema's read side ("the administrator feed and transition log", the signed worksheet), and PLAN-tushe-readonly-fold.md section 3.4 left the reader to be built after the delta file named the routes; #711 (ENV-05) merged on 18 Nov. The folding and the trace join stay the pure functions of noema#724; this module only reads.
- Guarantees, each tested: GET only, one request per read, no retry, no redirect followed; the key from a file, held only in a closure (in no error, repr or attribute); a failure is never "no rows" (REFUSED, NOT_FOUND, UNAVAILABLE, MALFORMED, INVALID); the feed pages on `sequence`, every sequence must strictly rise past the cursor, a feed not caught up within `max_pages` is an error and never a truncated list; ids and arguments are checked before any request.
- Not known, so required parameters with no default (as in the core's reader, noema#711): how the key is presented (`bearer` or `x-api-key`) and the envelope of each list (a bare array, or an object holding the array under a key). Nothing here was observed against Tushe.
- Tests: 26 written first and seen failing (module missing); 21 mutations of the reader each caught; mypy --strict and lint clean.
- Regression cases: none. Debt: none. Conflicts: none.

## Pre-work for the next agent
- Exact next step: when the Founder's read-only key exists (A240, noema#620) and the real envelope and presentation are known, set the two parameters and run the three acceptance reads (AT-05 delivery log and feed, AT-14 version-token conflicts on the case system, AT-17 the run record assembled from the scenario, checksum, feed and transition log) with `fold_transitions` and `join_trace` from noema#724.
- Open questions (owner): which of the eight scopes the two reads need (I expect `events:read` and `audit:read`; the others should not be asked for in the harness: Founders).
- Files to read first: `benchmark/oracle_client/PLAN-tushe-readonly-fold.md`, `benchmark/oracle_client/tushe_readonly.py`.
- Do not touch: the key value (I never see it); the frozen Tushe specification.

# Session 2026-11-19 (second) · BEN-03 reader · C1's independent read at 046c4098

## Session summary
- P2 (C1): a short page was read as "caught up", so a server that caps its page size below `limit` would silently truncate the feed. Fixed: `read_feed` ends only on an EMPTY page (an extra request at the end of every read); it never infers the end from a short page (AGENTS section 2, item 4). Tests: a server that caps the page size (limit 5, pages of 2) reads all five events in four requests; a page cap of 1 with limit 50 reads three events in four requests; the paging-path test now ends with the empty page. Written first (3 failing), 28 tests pass, mypy --strict and lint clean.
- P3 (C1): the `.gitleaksignore` line touches a security path: asked SR to read it (message of 19 Nov).
- Regression cases: none. Debt: none. Conflicts: none.

