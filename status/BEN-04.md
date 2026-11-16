# Status · BEN-04 (Benchmark Owner agent) · at least 50 adversarial fixtures, reproducible (G0-N item N10)

Updated 2026-11-16. Everything here is SYNTHETIC (AGENT-25): generated cases, fixtures and fault plans, no real data. Owner: the Founder (benchmark owner B). Agent: Benchmark Owner role agent. Task row: BEN-04 (plan; "50 cases validate; two runs give identical results; the runtime cannot read any oracle field"). Gate: G0-N (N10).

## State

- **The fixtures are generated, not committed.** Layer A is produced by `noema-casegen` 1.3.3: the same version and seed give byte-identical output. The generated directory is git-ignored (`benchmark/out/generated`), which is why the Steward found no `.json` under `benchmark/cases` on `main`: that is by design, not a gap. The evidence for "at least 50 fixtures, reproducible" is the command, the two run logs below and the digests, not 50 committed files. **I recommend the deciders read N10 that way and I do not recommend committing 320 generated files** (they would duplicate the generator and could drift from it; the golden manifest digest is already pinned by `benchmark/generator/test_generate.py` and checked on every CI run).
- **This run is local and tied to a commit.** It was made on Windows from `origin/main` at **`0aeb2ecb`** (the merge of noema#549), with the tracked files converted to LF so they match what CI checks out; no other change was in the tree. Generator `noema-casegen` 1.3.3. It is not a CI run and has no run link. **No CI job yet generates the suite twice and keeps the comparison.** The `bench-suites` workflow does not: its suites generate once, discard the generator log and run API cases, and it uploads no generated tree or digest. CI pins the manifest digest (`benchmark/generator/test_generate.py`, one process), and that test runs on pushes to `main` and on pull requests that change `benchmark/`, `contracts/schemas/benchmark*` or the benchmark workflow (not on a pull request that changes only this file). That is weaker than two separate generations compared. Writing the CI job is work still needed (see the next step). **Two independent readers (C1 on Linux, Voice and C2 on their machines) reproduced the digest, the 1,105 files and the byte identity.**

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

**Identity of the two runs:** both write the same manifest digest `sha256:c6bdd3a63e4da29487100251bef33ad1dba36561a17ecaa170dc211bc57f0191`, which is the golden digest pinned in `benchmark/generator/test_generate.py`. All 1,105 files of the two output trees are byte-identical (sha256 of every file compared). The tree digest is the sha256 of the lines `<relative path with forward slashes> <sha256 of the file>`, sorted by path, joined by a newline with no trailing newline: `54503ef7f9550a926a281d8ae95bc76b9b40f268f264c2eac7488084a112fa74`. `python -m unittest benchmark.generator.test_generate`: 18 tests OK (determinism, quotas, schema validity, oracle separation).

## What was checked

| Claim | Result |
|---|---|
| At least 50 cases | **320** GENERATED cases (layer counts: GENERATED 320); 320 fixtures; 320 oracle documents; 144 fault-plan files |
| Every case, oracle, fixture and the manifest validate against their schemas | 0 invalid documents (benchmark-case, benchmark-oracle, benchmark-fixture, benchmark-suite-manifest) |
| Coverage | the generator's own report: 0 gaps (US 22 of 22, PUS 8 of 8, EC 105 of 105, FAULT 29 of 29, CAP 20 of 20) |
| Each fixture names its fault source | every case has a `fault_plan` member (320 of 320). 144 plan files exist and all resolve (PROXY 89, TUSHE 32, MOCK_CASE 23); 175 cases name `fp_none`, correct for "no fault". **One exception: BEN-INJ-0003** ("inj case note") names `fp_none` and carries the marker `needs_fault:INJECTION_PAYLOAD.get_case`: no producer can inject the `case_note` of `get_case` today (the generator README says so). So injection in narration has two fully sourced cases and a third that is pending its fault source |
| The runtime cannot read any oracle field | the only key a case document shares with an oracle document is `case_id`; a case holds `oracle_ref` only, and the oracle documents are in a separate tree (`oracle/`). The runtime arm view and INV-22 checks are tested elsewhere; this run read no oracle value into this note. A case also carries an internal `expected` block that arms never see (it is projected away for arms). The tests behind this: `test_oracle_is_not_in_the_case_file` and `test_nothing_mentions_the_heldout_set` in `benchmark/generator/test_generate.py`, and the arm-view projection tests |

## The twelve kinds the BEN-04 row names, mapped to what the generator has

The mapping is **mine** (archetype names are the goal prefix of each case); a deciding reviewer should confirm each row. Corrected after C2's, Codex's and the Security Reviewer's reads: the two `tmo near timeout` archetypes were removed from row 3 because they fault `mock-case.create_case`, not the switch (they belong with row 5's case-creation timeouts), and "state switch failed credited" belongs to row 6 only. Row 3 now names the ten cases that fault `mock-switch` (service unavailable). **This is a gap against the plan's wording: no generated case injects a timeout or latency on the switch, and the deciders should say whether unavailability is enough or a switch-timeout fault is to be added to the generator (a generator change, new golden digest).** The other rows were not re-read by a second reader.

| BEN-04 kind | Archetypes used | Cases | First ids |
|---|---|---|---|
| two Tundes | amb two name; amb three similar names | 4 | BEN-AMB-0001, BEN-AMB-0019, BEN-AMB-0002 |
| duplicate amount and recipient | amb three same amount; state possible duplicate | 4 | BEN-AMB-0003, BEN-AMB-0021, BEN-STATE-0008 |
| switch timeout | **no case injects a timeout on the switch.** The switch cases are those with a fault on `mock-switch` that makes it unavailable: `host service unavailable` (BEN-DUP-1005, BEN-HOST-0008, BEN-HOST-1005, BEN-TMO-1009, BEN-TMO-1010; fault on `get_status`) and, on `get_settlement`, BEN-STATE-0010, BEN-STATE-0030, BEN-STATE-1001, BEN-STATE-1007, BEN-STATE-1013 | 10 | nearest to a switch timeout: an unavailable switch, not a slow one |
| unowned transaction | auth unowned transaction; auth other by name | 4 | BEN-AUTH-0001, BEN-AUTH-0020, BEN-AUTH-0002 |
| timeout after commit | tmo commit then drop; tmo 5xx after commit; tmo dropped response; tmo near timeout after; tmo near timeout before (all fault `mock-case.create_case`) | 8 | BEN-TMO-0001, BEN-TMO-0018, BEN-TMO-0005 |
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

- Exact next step (work still needed, not done): a small CI job (or `make` target plus a step in the `benchmark` job) that generates the suite twice into two directories with the same seed, compares every file's sha256, uploads the two logs and the tree digest as an artifact, and fails on any difference. Until it exists the N10 evidence is the local run above plus the pinned golden digest. Owner: BO (needs a workflow change, so a Founder approval and SR read of `.github`).
- Open questions (owner): whether the deciders accept "generated, reproducible, digest-pinned" for N10 (EL, IL, BO and the Founders).
- Files to read first: `benchmark/generator/README.md`, `benchmark/generator/test_generate.py`.
- Do not touch: the generator output of seed 20261020 (a change moves the golden digest and needs its own change).
