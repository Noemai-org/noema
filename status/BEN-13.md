# BEN-13: watch and callback cases (US-16, US-17) and the deadline paths (EC-T01 to EC-T11)

Owner: Benchmark Owner (role agent, generated layer only; INV-22). Date: 2026-11-12. Task row: BEN-13 in `docs/poc/execution/tasks.csv` (done when: "Cases validate"). This file is a log and a traceability record; it defines no requirement. Status of the task is the board's and the Repo Steward's, not this file's.

# Session 2026-11-12 14:00 WAT · BEN-13

## Session summary
- What changed (PR links): this file only (noema, `docs/BEN-13-15-trace`). No generator, contract or case changed.
- Why: the task asks that the cases validate and be linked to their user-story and edge-case IDs. The generator already builds them (the default seed, generator 1.3.3 on `main` at the time of writing; the suite digest is pinned in `benchmark/generator/test_generate.py`), and its coverage rule fails the build on a gap (`python -m benchmark.generator coverage`: 320 cases, 0 gaps; US 22 of 22, PUS 8 of 8, EC 105 of 105, FAULT 29 of 29, CAP 20 of 20). This record shows, per ID, which cases cover it, so a reviewer can find them.
- Risks, and how they are contained: coverage by count is not coverage by depth. US-16 has 3 cases and US-17 has 2: they meet the coverage rule (at least one) but are thin. The deadline paths are the policy and session edge cases EC-T01 to EC-T11 of the catalogue (my reading of "deadline paths"; if the task meant others, name them).
- Tests added (REQ, INV, US or EC IDs): none new. Existing: `test_generate.py` (every case and oracle validates against its contract; oracle, fixture and fault-plan consistency; the pinned manifest digest), `test_coverage.py` (the report fails when a story, capability or family is removed).
- Regression cases added: none.
- Debt entries: none.
- Conflicts raised: none.

## Traceability (from the suite manifest `covers`, the default seed)
| ID | Cases | Case IDs (first six) |
|---|---|---|
| US-16 | 3 | BEN-TMO-0004, BEN-TMO-0021, BEN-TMO-1004 |
| US-17 | 2 | BEN-TMO-0009, BEN-TMO-0010 |
| EC-T01 | 3 | BEN-POL-1003, BEN-TMO-0008, BEN-TMO-0025 |
| EC-T02 | 2 | BEN-POL-0003, BEN-POL-0012 |
| EC-T03 | 2 | BEN-POL-0001, BEN-POL-0010 |
| EC-T04 | 4 | BEN-POL-0002, BEN-POL-0011, BEN-POL-1001, BEN-POL-1005 |
| EC-T05 | 2 | BEN-POL-0004, BEN-POL-0013 |
| EC-T06 | 4 | BEN-SESS-0002, BEN-SESS-0008, BEN-SESS-1001, BEN-SESS-1003 |
| EC-T07 | 2 | BEN-POL-0005, BEN-POL-0014 |
| EC-T08 | 2 | BEN-POL-0006, BEN-POL-1004 |
| EC-T09 | 2 | BEN-TMO-0011, BEN-TMO-0012 |
| EC-T10 | 1 | BEN-TMO-0013 |
| EC-T11 | 1 | BEN-TMO-0014 |

## Pre-work for the next agent
- Exact next step: add an archetype only where a depth gap is named by a reviewer; every addition bumps `GENERATOR_VERSION` and the pinned digest with a reason (generator README).
- Open questions (owner): none.
- Files to read first: `benchmark/generator/catalogue.py`, `coverage.py`, `README.md`.
- Do not touch: `benchmark-heldout` (INV-22); the oracle files from any agent view.
