# Status · BEN-12 (Benchmark Owner agent) · the G2 table by user story and family

# Session 2026-11-17 · BEN-12 · the table producer is built; the run waits for the stack

## Session summary
- What changed: `benchmark/scoring/story_table.py` (pure `story_table`, `from_run`, `render`), a `--story-table` flag on the scorer, and every suite run now passes it (so the table comes from the same run); 15 tests.
- Why: ruling A300: one row per user story plus a by-family rollup, a pure function over the case judgements and the manifest. G2's evidence is "Benchmark smoke run by family; CI".
- Rule: a story PASSES only when it has at least one case and every case completed, was handled and tripped no gate. FAIL outranks INCOMPLETE; a story with no case is NO_CASE, never a pass. The G2 verdict is ALL_PASS only when the 15 stories G2 names all pass. It adds up what the scorer decided and holds no threshold (AGENT-16). The gate's other conditions (write capabilities, kill switch, conformance packs) are not in this table.
- Not done: the run itself. BEN-12 ("Table produced from the run ID") needs the stack up and TST-02 merged (#675); until then no table exists and none is claimed.
- Tests: 14 in `test_story_table.py` (seen red first), 1 in `test_cli.py`, 1 in `test_select.py`; ten mutations of `story_table.py` each caught.
- Regression cases: none. Debt: none. Conflicts: none.

## Pre-work for the next agent
- Exact next step: when the stack is up, `python -m benchmark.suites run smoke` (or `us`), and attach `story-table.md` for the run ID to the G2 gate record.
- Open questions (owner): none new.
- Files to read first: `benchmark/scoring/story_table.py`.
- Do not touch: the scorecard v3 contract.
