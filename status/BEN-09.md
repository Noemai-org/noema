# Status · BEN-09 (Benchmark Owner agent) · integration-hostility and duplicate cases

Updated 2026-11-17. Owner: the Founder (benchmark owner B). Agent: Benchmark Owner role agent. Needs: BEN-04 (done: noema#590 merged, noema#626 open). Plan row: "US-05, US-21, EC-S19 and EC-E01 to EC-E16"; done when "Cases validate".

## State

- **The cases are generated, not committed** (`noema-casegen` 1.3.3, seed 20261020, 320 cases; the manifest digest `sha256:c6bdd3a63e4da29487100251bef33ad1dba36561a17ecaa170dc211bc57f0191` is pinned by `benchmark/generator/test_generate.py` and, in CI, compared across two separate processes by noema#626). "Cases validate" is shown by the generator tests (every case, oracle and fixture validates against its schema) and by the coverage command below. Each case is linked to its identifiers by the coverage tags of its manifest entry.
- **Every identifier of this task is carried by at least one case: 0 gaps** across 19 identifiers, by 71 distinct cases. 1 identifier(s) are carried by exactly one case ("thin"); the list is in the table. A thin identifier is covered, but a single case does not show it is not a one-off.
- Reproduce: `python -m benchmark.generator.task_coverage --task BEN-09` (exit 1 on any gap). The table below is its output at the commit that adds this file.

## BEN-09: integration-hostility and duplicate cases: coverage by identifier (SYNTHETIC generated suite; tags and case ids only)

| ID | Cases | Depth | First case ids |
|---|---|---|---|
| EC-E01 | 6 | ok | BEN-TMO-0001, BEN-TMO-0017, BEN-TMO-0018 |
| EC-E02 | 2 | ok | BEN-TMO-0002, BEN-TMO-0019 |
| EC-E03 | 2 | ok | BEN-TMO-0003, BEN-TMO-0020 |
| EC-E04 | 4 | ok | BEN-INJ-0016, BEN-TMO-0004, BEN-TMO-0021 |
| EC-E05 | 8 | ok | BEN-DUP-0001, BEN-DUP-0002, BEN-DUP-0009 |
| EC-E06 | 3 | ok | BEN-DUP-0003, BEN-DUP-0011, BEN-DUP-0019 |
| EC-E07 | 5 | ok | BEN-DUP-0004, BEN-DUP-0012, BEN-DUP-0020 |
| EC-E08 | 3 | ok | BEN-HOST-0007, BEN-HOST-1006, BEN-TMO-0016 |
| EC-E09 | 4 | ok | BEN-TMO-0006, BEN-TMO-0023, BEN-TMO-1003 |
| EC-E10 | 2 | ok | BEN-TMO-0007, BEN-TMO-0024 |
| EC-E11 | 8 | ok | BEN-DUP-0007, BEN-DUP-0015, BEN-DUP-1001 |
| EC-E12 | 6 | ok | BEN-TMO-0005, BEN-TMO-0022, BEN-TMO-1002 |
| EC-E13 | 2 | ok | BEN-HOST-0003, BEN-HOST-0013 |
| EC-E14 | 5 | ok | BEN-HOST-0002, BEN-HOST-0012, BEN-HOST-1001 |
| EC-E15 | 3 | ok | BEN-SESS-0005, BEN-SESS-0011, BEN-SESS-1004 |
| EC-E16 | 1 | thin | BEN-TMO-0015 |
| EC-S19 | 8 | ok | BEN-DUP-1003, BEN-HOST-0004, BEN-HOST-0014 |
| US-05 | 6 | ok | BEN-DUP-0001, BEN-DUP-0008, BEN-DUP-0009 |
| US-21 | 2 | ok | BEN-HOST-0001, BEN-HOST-0011 |

identifiers: 19; gaps: 0; thin (exactly one case): 1
thin: EC-E16

## What this does not show

- It does not show that the cases are **right**. The expectations of some archetypes are INFERRED where the PERD allows more than one reading (`python -m benchmark.generator assumptions` lists them for the Benchmark Owner to confirm; that confirmation is mine and is not done by this note).
- It does not show that the cases pass: that needs a run through the public API (BEN-05, G1).
- Depth is counted in cases, not in distinct situations: two cases of one archetype count twice.

## Pre-work for the next agent

- Exact next step: the Founder or the Architect, if they want more than one case for a thin identifier, say so; that is a generator change and a new golden digest.
- Open questions (owner): whether "thin" is enough for the identifiers listed (the deciders).
- Files to read first: `benchmark/generator/task_coverage.py`, `benchmark/generator/README.md`.
- Do not touch: the generator output of seed 20261020 without a change that moves the golden digest on purpose.
