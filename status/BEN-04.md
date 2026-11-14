# Status · BEN-04 (Benchmark Owner agent) · at least 50 adversarial fixtures, reproducible (G0-N item N10)

Updated 2026-11-14. Owner: the Founder (benchmark owner B). Agent: Benchmark Owner role agent. Task row: BEN-04 (plan; "50 cases validate; two runs give identical results; the runtime cannot read any oracle field"). Gate: G0-N (N10).

## State

- **The fixtures are generated, not committed.** Layer A is produced by `noema-casegen` 1.3.3: the same version and seed give byte-identical output. The generated directory is git-ignored (`benchmark/out/generated`), which is why the Steward found no `.json` under `benchmark/cases` on `main`: that is by design, not a gap. The evidence for "at least 50 fixtures, reproducible" is the command, the two run logs below and the digests, not 50 committed files. **I recommend the deciders read N10 that way and I do not recommend committing 320 generated files** (they would duplicate the generator and could drift from it; the golden manifest digest is already pinned by `benchmark/generator/test_generate.py` and checked on every CI run).
- **This run is local** (Windows, a work tree converted to LF so it matches what CI checks out), not a CI run. It is shown as what exists today. A run on a Linux runner at a final head, linked, is stronger; the Founder can dispatch the `bench-suites` workflow for it.

## The two fixture runs (command, then the logs verbatim)

```
python -m benchmark.generator generate --seed 20261020 --out <out1>      # run 1, exit 0
python -m benchmark.generator generate --seed 20261020 --out <out2>      # run 2, exit 0
```

Run 1 log:

```
coverage: 320 cases, 0 gaps
  US: 22 of 22 covered
  PUS: 8 of 8 covered
  EC: 105 of 105 covered
  FAULT: 29 of 29 covered
  CAP: 20 of 20 covered
noema-casegen 1.3.3 seed 20261020: 320 cases written to <out1> (manifest sha256:c6bdd3a63e4da29487100251bef33ad1dba36561a17ecaa170dc211bc57f0191)
```

Run 2 log:

```
coverage: 320 cases, 0 gaps
  US: 22 of 22 covered
  PUS: 8 of 8 covered
  EC: 105 of 105 covered
  FAULT: 29 of 29 covered
  CAP: 20 of 20 covered
noema-casegen 1.3.3 seed 20261020: 320 cases written to <out2> (manifest sha256:c6bdd3a63e4da29487100251bef33ad1dba36561a17ecaa170dc211bc57f0191)
```

**Identity of the two runs:** both write the same manifest digest `sha256:c6bdd3a63e4da29487100251bef33ad1dba36561a17ecaa170dc211bc57f0191`, which is the golden digest pinned in `benchmark/generator/test_generate.py`. All 1,105 files of the two output trees are byte-identical (sha256 of every file compared). The digest of the sorted `<path> <sha256>` lines of the tree is `54503ef7f9550a926a281d8ae95bc76b9b40f268f264c2eac7488084a112fa74`. `python -m unittest benchmark.generator.test_generate`: 18 tests OK (determinism, quotas, schema validity, oracle separation).

## What was checked

| Claim | Result |
|---|---|
| At least 50 cases | **320** GENERATED cases (layer counts: GENERATED 320); 320 fixtures; 320 oracle documents; 144 fault-plan files |
| Every case, oracle, fixture and the manifest validate against their schemas | 0 invalid documents (benchmark-case, benchmark-oracle, benchmark-fixture, benchmark-suite-manifest) |
| Coverage | the generator's own report: 0 gaps (US 22 of 22, PUS 8 of 8, EC 105 of 105, FAULT 29 of 29, CAP 20 of 20) |
| Each fixture names its fault source | every case has a `fault_plan` member (320 of 320; `NONE` where there is no fault) |
| The runtime cannot read any oracle field | the only key a case document shares with an oracle document is `case_id`; a case holds `oracle_ref` only, and the oracle documents are in a separate tree (`oracle/`). The runtime arm view and INV-22 checks are tested elsewhere; this run read no oracle value into this note |

## The twelve kinds the BEN-04 row names, mapped to what the generator has

The mapping is **mine** (archetype names are the goal prefix of each case); a deciding reviewer should confirm each row, especially the third and sixth, where the plan's wording and the archetype names are not identical.

| BEN-04 kind | Archetypes used | Cases | First ids |
|---|---|---|---|
| two Tundes | amb two name; amb three similar names | 4 | BEN-AMB-0001, BEN-AMB-0019, BEN-AMB-0002 |
| duplicate amount and recipient | amb three same amount; state possible duplicate | 4 | BEN-AMB-0003, BEN-AMB-0021, BEN-STATE-0008 |
| switch timeout | state switch failed credited; tmo near timeout after; tmo near timeout before | 5 | BEN-STATE-0002, BEN-STATE-0022, BEN-STATE-0042 |
| unowned transaction | auth unowned transaction; auth other by name | 4 | BEN-AUTH-0001, BEN-AUTH-0020, BEN-AUTH-0002 |
| timeout after commit | tmo commit then drop; tmo 5xx after commit; tmo dropped response | 6 | BEN-TMO-0001, BEN-TMO-0018, BEN-TMO-0005 |
| contradictory destination credit | state failed debit credited; state switch failed credited; amb contradiction | 9 | BEN-DUP-1007, BEN-STATE-0003, BEN-STATE-0023 |
| stale evidence | state stale refresh; host stale read; state aged before execution | 6 | BEN-STATE-0011, BEN-STATE-0031, BEN-HOST-0010 |
| prohibited reversal | auth reverse it; auth refund now; inj evil prohibited remedy | 3 | BEN-AUTH-0012, BEN-AUTH-0013, BEN-INJ-0014 |
| duplicate request | dup replayed request; dup double tap; dup asked twice; dup asked three times | 14 | BEN-DUP-0005, BEN-DUP-0013, BEN-DUP-0021 |
| policy mutation | pol publish after grant; pol publish before decision; pol outage during publish | 5 | BEN-POL-0002, BEN-POL-0011, BEN-POL-0001 |
| injection in narration | inj narration; inj beneficiary name; inj case note | 3 | BEN-INJ-0001, BEN-INJ-0002, BEN-INJ-0003 |
| callback replay | dup webhook noise; state late webhook; host dropped webhook | 7 | BEN-DUP-0004, BEN-DUP-0012, BEN-DUP-0020 |

## What this does not show (said plainly)

- It is not a run through the public API: it shows the fixtures exist, validate and reproduce. The runs of US-01 and US-06 through the API are BEN-05 (G1), which needs the stack (noema#436).
- It does not show N12 (AT-04, AT-05, AT-07, AT-14 shown internally): those are tests of the case system and the proxy (IL) and the runner (BO); none is recorded yet.
- It does not decide G0-N. The deciders read it at the review and fill the gate record.

## Pre-work for the next agent

- Exact next step: a CI run of the same two generations at a final head (dispatch `bench-suites`, Founder), linked here.
- Open questions (owner): whether the deciders accept "generated, reproducible, digest-pinned" for N10 (EL, IL, BO and the Founders).
- Files to read first: `benchmark/generator/README.md`, `benchmark/generator/test_generate.py`.
- Do not touch: the generator output of seed 20261020 (a change moves the golden digest and needs its own change).
