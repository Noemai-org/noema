# Status · BEN-07 (Benchmark Owner agent) · state-conflict cases

Updated 2026-11-17. Owner: the Founder (benchmark owner B). Agent: Benchmark Owner role agent. Needs: BEN-04 (done: noema#590 merged, noema#626 open). Plan row: "US-03, US-04, US-13, US-14 and EC-S01 to EC-S22"; done when "Cases validate".

## State

- **The cases are generated, not committed** (`noema-casegen` 1.3.3, seed 20261020, 320 cases; the manifest digest `sha256:c6bdd3a63e4da29487100251bef33ad1dba36561a17ecaa170dc211bc57f0191` is pinned by `benchmark/generator/test_generate.py` and, in CI, compared across two separate processes by noema#626). "Cases validate" is shown by the generator tests (every case, oracle and fixture validates against its schema) and by the coverage command below. Each case is linked to its identifiers by the coverage tags of its manifest entry.
- **Every identifier of this task is carried by at least one case: 0 gaps** across 26 identifiers, by 90 distinct cases. 0 identifier(s) are carried by exactly one case ("thin"); the list is in the table. A thin identifier is covered, but a single case does not show it is not a one-off.
- Reproduce: `python -m benchmark.generator.task_coverage --task BEN-07` (exit 1 on any gap). The table below is its output at the commit that adds this file.

## BEN-07: state-conflict cases: coverage by identifier (SYNTHETIC generated suite; tags and case ids only)

| ID | Cases | Depth | First case ids |
|---|---|---|---|
| EC-S01 | 3 | ok | BEN-STATE-0001, BEN-STATE-0021, BEN-STATE-0041 |
| EC-S02 | 4 | ok | BEN-CLEAN-0001, BEN-CLEAN-0014, BEN-CLEAN-0027 |
| EC-S03 | 6 | ok | BEN-STATE-0002, BEN-STATE-0022, BEN-STATE-0042 |
| EC-S04 | 4 | ok | BEN-DUP-1007, BEN-STATE-0003, BEN-STATE-0023 |
| EC-S05 | 2 | ok | BEN-STATE-0004, BEN-STATE-0024 |
| EC-S06 | 2 | ok | BEN-STATE-0005, BEN-STATE-0025 |
| EC-S07 | 5 | ok | BEN-STATE-0006, BEN-STATE-0026, BEN-STATE-1004 |
| EC-S08 | 2 | ok | BEN-STATE-0007, BEN-STATE-0027 |
| EC-S09 | 5 | ok | BEN-STATE-0008, BEN-STATE-0028, BEN-STATE-1005 |
| EC-S10 | 2 | ok | BEN-STATE-0009, BEN-STATE-0029 |
| EC-S11 | 10 | ok | BEN-DUP-1005, BEN-HOST-0008, BEN-HOST-1005 |
| EC-S12 | 13 | ok | BEN-DUP-1001, BEN-HOST-0006, BEN-HOST-0010 |
| EC-S13 | 2 | ok | BEN-STATE-0011, BEN-STATE-0031 |
| EC-S14 | 3 | ok | BEN-STATE-0013, BEN-STATE-0033, BEN-STATE-1006 |
| EC-S15 | 2 | ok | BEN-STATE-0012, BEN-STATE-0032 |
| EC-S16 | 2 | ok | BEN-STATE-0014, BEN-STATE-0034 |
| EC-S17 | 2 | ok | BEN-STATE-0015, BEN-STATE-0035 |
| EC-S18 | 4 | ok | BEN-HOST-0005, BEN-HOST-1002, BEN-POL-1002 |
| EC-S19 | 8 | ok | BEN-DUP-1003, BEN-HOST-0004, BEN-HOST-0014 |
| EC-S20 | 3 | ok | BEN-DUP-1009, BEN-STATE-0016, BEN-STATE-0036 |
| EC-S21 | 2 | ok | BEN-STATE-0017, BEN-STATE-0037 |
| EC-S22 | 3 | ok | BEN-STATE-0018, BEN-STATE-0038, BEN-STATE-1012 |
| US-03 | 3 | ok | BEN-STATE-0001, BEN-STATE-0021, BEN-STATE-0041 |
| US-04 | 3 | ok | BEN-AUTH-0014, BEN-STATE-0019, BEN-STATE-0039 |
| US-13 | 2 | ok | BEN-STATE-0006, BEN-STATE-0026 |
| US-14 | 3 | ok | BEN-STATE-0002, BEN-STATE-0022, BEN-STATE-0042 |

identifiers: 26; gaps: 0; thin (exactly one case): 0

## What this does not show

- It does not show that the cases are **right**. The expectations of some archetypes are INFERRED where the PERD allows more than one reading (`python -m benchmark.generator assumptions` lists them for the Benchmark Owner to confirm; that confirmation is mine and is not done by this note).
- It does not show that the cases pass: that needs a run through the public API (BEN-05, G1).
- Depth is counted in cases, not in distinct situations: two cases of one archetype count twice.

## Pre-work for the next agent

- Exact next step: the Founder or the Architect, if they want more than one case for a thin identifier, say so; that is a generator change and a new golden digest.
- Open questions (owner): whether "thin" is enough for the identifiers listed (the deciders).
- Files to read first: `benchmark/generator/task_coverage.py`, `benchmark/generator/README.md`.
- Do not touch: the generator output of seed 20261020 without a change that moves the golden digest on purpose.
