# BEN-15: injection and red-team cases (US-09, EC-X01 to EC-X19; ADV-11 to ADV-13)

Owner: Benchmark Owner (role agent, generated layer only; INV-22). Date: 2026-11-12. Task row: BEN-15 in `docs/poc/execution/tasks.csv` (done when: "Cases validate"). This file is a log and a traceability record; it defines no requirement. Status of the task is the board's and the Repo Steward's, not this file's.

# Session 2026-11-12 14:00 WAT · BEN-15

## Session summary
- What changed (PR links): this file only (noema, `docs/BEN-13-15-trace`). No generator, contract or case changed.
- Why: the task asks that the cases validate and be linked to their user-story and edge-case IDs. The generator already builds them (the default seed, generator 1.3.3 on `main` at the time of writing; the suite digest is pinned in `benchmark/generator/test_generate.py`), and its coverage rule fails the build on a gap (`python -m benchmark.generator coverage`: 320 cases, 0 gaps; US 22 of 22, PUS 8 of 8, EC 105 of 105, FAULT 29 of 29, CAP 20 of 20). This record shows, per ID, which cases cover it, so a reviewer can find them.
- Risks, and how they are contained: coverage by count is not coverage by depth. US-09 has 1 case (BEN-INJ-0001). ADV-11 to ADV-13 are PERD adversarial tests, not catalogue IDs; the generator expresses them through the PROMPT_INJECTION category (20 cases) and the AUTHORISATION category (30 cases), 32 cases that use the fault INJECTION_PAYLOAD, and families B6 and B3 (PERD table: ADV-11 B6; ADV-12 B3, B6; ADV-13 B6 and scanners). That mapping is my reading; the scanner half of ADV-13 is not a case.
- Tests added (REQ, INV, US or EC IDs): none new. Existing: `test_generate.py` (every case and oracle validates against its contract; oracle, fixture and fault-plan consistency; the pinned manifest digest), `test_coverage.py` (the report fails when a story, capability or family is removed).
- Regression cases added: none.
- Debt entries: none.
- Conflicts raised: none.

## Traceability (from the suite manifest `covers`, the default seed)
| ID | Cases | Case IDs (first six) |
|---|---|---|
| US-09 | 1 | BEN-INJ-0001 |
| EC-X01 | 2 | BEN-AUTH-0002, BEN-AUTH-0021 |
| EC-X02 | 1 | BEN-INJ-0001 |
| EC-X03 | 29 | BEN-AMB-1006, BEN-AMB-1012, BEN-AUTH-1003, BEN-AUTH-1006, BEN-AUTH-1009, BEN-CLEAN-1001 … |
| EC-X04 | 1 | BEN-INJ-0003 |
| EC-X05 | 1 | BEN-AUTH-0013 |
| EC-X06 | 2 | BEN-AUTH-0007, BEN-AUTH-1009 |
| EC-X07 | 3 | BEN-DUP-0005, BEN-DUP-0013, BEN-DUP-0021 |
| EC-X08 | 2 | BEN-AUTH-0008, BEN-INJ-0013 |
| EC-X09 | 1 | BEN-AUTH-0010 |
| EC-X10 | 1 | BEN-INJ-0010 |
| EC-X11 | 1 | BEN-INJ-0011 |
| EC-X12 | 2 | BEN-AUTH-0009, BEN-AUTH-1003 |
| EC-X13 | 1 | BEN-INJ-0005 |
| EC-X14 | 1 | BEN-INJ-0006 |
| EC-X15 | 1 | BEN-INJ-0007 |
| EC-X16 | 1 | BEN-INJ-0008 |
| EC-X17 | 1 | BEN-INJ-0009 |
| EC-X18 | 1 | BEN-INJ-0012 |
| EC-X19 | 1 | BEN-INJ-0019 |

## Pre-work for the next agent
- Exact next step: add an archetype only where a depth gap is named by a reviewer; every addition bumps `GENERATOR_VERSION` and the pinned digest with a reason (generator README).
- Open questions (owner): none.
- Files to read first: `benchmark/generator/catalogue.py`, `coverage.py`, `README.md`.
- Do not touch: `benchmark-heldout` (INV-22); the oracle files from any agent view.
