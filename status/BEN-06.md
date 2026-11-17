# Status · BEN-06 (Benchmark Owner agent) · ambiguity and confused-deputy cases

Updated 2026-11-17. Owner: the Founder (benchmark owner B). Agent: Benchmark Owner role agent. Needs: BEN-04 (done: noema#590 merged, noema#626 open). Plan row: "EC-A01 to EC-A20, US-02, US-07, PUS-01, PUS-02, PUS-08"; done when "Cases validate"; each is linked to its EC or US ID.

## State

- **The cases are generated, not committed** (`noema-casegen` 1.3.3, seed 20261020, 320 cases; the manifest digest `sha256:c6bdd3a63e4da29487100251bef33ad1dba36561a17ecaa170dc211bc57f0191` is pinned by `benchmark/generator/test_generate.py` and, in CI, compared across two separate processes by noema#626). "Cases validate" is shown by the generator tests (every case, oracle and fixture validates against its schema) and by the coverage command below. Each case is linked to its identifiers by the coverage tags of its manifest entry.
- **Every identifier of this task is carried by at least one case: 0 gaps** across 25 identifiers, by 84 distinct cases. 0 identifier(s) are carried by exactly one case ("thin"); the list is in the table. A thin identifier is covered, but a single case does not show it is not a one-off.
- Reproduce: `python -m benchmark.generator.task_coverage --task BEN-06` (exit 1 on any gap). The table below is its output at the commit that adds this file.

## BEN-06: ambiguity and confused-deputy cases: coverage by identifier (SYNTHETIC generated suite; tags and case ids only)

| ID | Cases | Depth | First case ids |
|---|---|---|---|
| EC-A01 | 4 | ok | BEN-AMB-0001, BEN-AMB-0014, BEN-AMB-0019 |
| EC-A02 | 2 | ok | BEN-AMB-0002, BEN-AMB-0020 |
| EC-A03 | 2 | ok | BEN-AMB-0003, BEN-AMB-0021 |
| EC-A04 | 6 | ok | BEN-AMB-0004, BEN-AMB-0005, BEN-AMB-0022 |
| EC-A05 | 6 | ok | BEN-AMB-1004, BEN-AMB-1010, BEN-CLEAN-0005 |
| EC-A06 | 11 | ok | BEN-AMB-1009, BEN-CLEAN-0004, BEN-CLEAN-0017 |
| EC-A07 | 7 | ok | BEN-AMB-0006, BEN-AMB-0024, BEN-AMB-1002 |
| EC-A08 | 6 | ok | BEN-AMB-1003, BEN-AMB-1009, BEN-AMB-1015 |
| EC-A09 | 2 | ok | BEN-AMB-0007, BEN-AMB-0025 |
| EC-A10 | 2 | ok | BEN-AMB-0008, BEN-AMB-0026 |
| EC-A11 | 2 | ok | BEN-AMB-0009, BEN-AMB-0027 |
| EC-A12 | 2 | ok | BEN-AMB-0010, BEN-AMB-0028 |
| EC-A13 | 2 | ok | BEN-AUTH-0001, BEN-AUTH-0020 |
| EC-A14 | 2 | ok | BEN-AMB-0011, BEN-AMB-0029 |
| EC-A15 | 3 | ok | BEN-AMB-0012, BEN-AMB-0030, BEN-INJ-0018 |
| EC-A16 | 3 | ok | BEN-AMB-0013, BEN-AMB-0031, BEN-INJ-0004 |
| EC-A17 | 8 | ok | BEN-AMB-1013, BEN-AMB-1014, BEN-AUTH-1007 |
| EC-A18 | 2 | ok | BEN-CLEAN-0007, BEN-CLEAN-0020 |
| EC-A19 | 3 | ok | BEN-CLEAN-0008, BEN-CLEAN-0021, BEN-CLEAN-1001 |
| EC-A20 | 4 | ok | BEN-CLEAN-0002, BEN-CLEAN-0015, BEN-CLEAN-0028 |
| PUS-01 | 2 | ok | BEN-AMB-0011, BEN-AMB-0029 |
| PUS-02 | 2 | ok | BEN-AMB-0012, BEN-AMB-0030 |
| PUS-08 | 2 | ok | BEN-AMB-0015, BEN-AMB-0033 |
| US-02 | 4 | ok | BEN-AMB-0001, BEN-AMB-0014, BEN-AMB-0019 |
| US-07 | 8 | ok | BEN-AUTH-0001, BEN-AUTH-0020, BEN-AUTH-1001 |

identifiers: 25; gaps: 0; thin (exactly one case): 0

## What this does not show

- It does not show that the cases are **right**. The expectations of some archetypes are INFERRED where the PERD allows more than one reading (`python -m benchmark.generator assumptions` lists them for the Benchmark Owner to confirm; that confirmation is mine and is not done by this note).
- It does not show that the cases pass: that needs a run through the public API (BEN-05, G1).
- Depth is counted in cases, not in distinct situations: two cases of one archetype count twice.

## Pre-work for the next agent

- Exact next step: the Founder or the Architect, if they want more than one case for a thin identifier, say so; that is a generator change and a new golden digest.
- Open questions (owner): whether "thin" is enough for the identifiers listed (the deciders).
- Files to read first: `benchmark/generator/task_coverage.py`, `benchmark/generator/README.md`.
- Do not touch: the generator output of seed 20261020 without a change that moves the golden digest on purpose.
