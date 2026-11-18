# Status · BEN-08 (Benchmark Owner agent) · policy-mutation and clock cases

Updated 2026-11-17. Owner: the Founder (benchmark owner B). Agent: Benchmark Owner role agent. Needs: BEN-04 (done: noema#590 merged, noema#626 open). Plan row: "US-08, US-18, US-20 and EC-T01 to EC-T11"; done when "Cases validate".

## State

- **The cases are generated, not committed** (`noema-casegen` 1.3.3, seed 20261020, 320 cases; the manifest digest `sha256:c6bdd3a63e4da29487100251bef33ad1dba36561a17ecaa170dc211bc57f0191` is pinned by `benchmark/generator/test_generate.py` and, in CI, compared across two separate processes by noema#626). "Cases validate" is shown by the generator tests (every case, oracle and fixture validates against its schema) and by the coverage command below. Each case is linked to its identifiers by the coverage tags of its manifest entry.
- **Every identifier of this task is carried by at least one case: 0 gaps** across 14 identifiers, by 26 distinct cases. 3 identifier(s) are carried by exactly one case ("thin"); the list is in the table. A thin identifier is covered, but a single case does not show it is not a one-off.
- Reproduce: `python -m benchmark.generator.task_coverage --task BEN-08` (exit 1 on any gap). The table below is its output at the commit that adds this file.

## BEN-08: policy-mutation and clock cases: coverage by identifier (SYNTHETIC generated suite; tags and case ids only)

| ID | Cases | Depth | First case ids |
|---|---|---|---|
| EC-T01 | 3 | ok | BEN-POL-1003, BEN-TMO-0008, BEN-TMO-0025 |
| EC-T02 | 2 | ok | BEN-POL-0003, BEN-POL-0012 |
| EC-T03 | 2 | ok | BEN-POL-0001, BEN-POL-0010 |
| EC-T04 | 4 | ok | BEN-POL-0002, BEN-POL-0011, BEN-POL-1001 |
| EC-T05 | 2 | ok | BEN-POL-0004, BEN-POL-0013 |
| EC-T06 | 4 | ok | BEN-SESS-0002, BEN-SESS-0008, BEN-SESS-1001 |
| EC-T07 | 2 | ok | BEN-POL-0005, BEN-POL-0014 |
| EC-T08 | 2 | ok | BEN-POL-0006, BEN-POL-1004 |
| EC-T09 | 2 | ok | BEN-TMO-0011, BEN-TMO-0012 |
| EC-T10 | 1 | thin | BEN-TMO-0013 |
| EC-T11 | 1 | thin | BEN-TMO-0014 |
| US-08 | 2 | ok | BEN-POL-0002, BEN-POL-0011 |
| US-18 | 1 | thin | BEN-POL-0007 |
| US-20 | 2 | ok | BEN-POL-0002, BEN-POL-0011 |

identifiers: 14; gaps: 0; thin (exactly one case): 3
thin: EC-T10, EC-T11, US-18

## What this does not show

- It does not show that the cases are **right**. The expectations of some archetypes are INFERRED where the PERD allows more than one reading (`python -m benchmark.generator assumptions` lists them for the Benchmark Owner to confirm; that confirmation is mine and is not done by this note).
- It does not show that the cases pass: that needs a run through the public API (BEN-05, G1).
- Depth is counted in cases, not in distinct situations: two cases of one archetype count twice.

## Pre-work for the next agent

- Exact next step: the Founder or the Architect, if they want more than one case for a thin identifier, say so; that is a generator change and a new golden digest.
- Open questions (owner): whether "thin" is enough for the identifiers listed (the deciders).
- Files to read first: `benchmark/generator/task_coverage.py`, `benchmark/generator/README.md`.
- Do not touch: the generator output of seed 20261020 without a change that moves the golden digest on purpose.
