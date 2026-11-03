# Status · BEN-30 (Benchmark Owner agent) · consistency and disambiguation diagnostics

Updated 2026-11-03. Owner: the Founder (benchmark owner B). Needs: BEN-01. Gate: none (diagnostics, not gates). REQ-BEN-009.

## State

- **Built on a local branch, not yet pushed** (`feat/BEN-30-diagnostics`, off `main` b2047e4): `benchmark-case-result.v2` (adds `turns`) and `benchmark-scorecard.v2` (adds `pass_k`, `turns_to_resolution`, `unnecessary_clarification`, `auto_resolve_precision`, `definitions` under `tier1.diagnostics`) with fixtures, an inventory row, a migration note and `tools/contracts/test_benchmark_v2.py`; `benchmark/scoring/diagnostics.py`; the runner counts turns; the scoring report prints the four diagnostics; `--repeat-results` on the scoring CLI. Local: `unittest discover` 544 tests OK and `mypy --strict` clean on `benchmark`; `tools/contracts/validate.py` passes.
- **Order of work.** Tests were written first for the scorer, report, CLI and runner and seen failing. The two schemas and their fixtures were written before `test_benchmark_v2.py`, which pins v1 bytes and checks v2 is v1 plus exactly the additions; the fixtures (accepted and rejected for a named reason) are the failing-first evidence for the schemas themselves.
- **Approvals.** The Architect approved the two v2 contracts, the `turns` field and runner counting on 2 Nov 2026, and accepted the proposed definitions as pre-registered (published in `benchmark/scoring/README.md`). Live model runs stay unapproved; nothing here ran a model.

## Open

1. ~~B2 in the Understanding-dependent set.~~ Closed 3 Nov 2026: the Founder decided B1 only (SoT §38). `UNDERSTANDING_FAMILIES = ("B1",)`, definitions version 2026-11-03, before any live run.
2. **noema#102 (`benchmark/replay`)** validates results against `benchmark-case-result` v1. Founder's order (3 Nov): #102 merges first, then this PR moves `benchmark/replay` to v2 in the same PR. Not stackable earlier (no PR is based on another PR's branch); done the moment #102 is on `main`.
3. **Real values** need five live runs per case. No run, no provider call and no spend by this work.
4. **Rule of three on tiny n** prints values above 100% (for example 150% at n = 2); that is existing scorer behaviour, unchanged.
5. **Branch.** This work is on a new local branch; pushing it and opening a PR needs the Architect's or founders' go-ahead for the branch name (the session's designated branch carries #102).

## Messages

### 2026-11-02 (time not recorded) accepted · Architect (relayed by the user) → Benchmark Owner agent
- Purpose: BEN-30 approved to add `benchmark-scorecard.v2` (four diagnostics under `tier1.diagnostics`) and `turns` in `benchmark-case-result.v2`, with the runner counting turns; the proposed definitions accepted as pre-registered, published in the scorecard documentation before any live run; live model runs unapproved; build the pass^k calculator on synthetic data.
- Receiver used it for: this work.
- Evidence: the commits on `feat/BEN-30-diagnostics`.

### 2026-11-03 (time not recorded) accepted · Founder (work order, relayed by the user) → Benchmark Owner agent
- Purpose: push `feat/BEN-30-diagnostics` and open its PR (done: noema#112); Understanding-dependent means family B1 only; #102 merges first, then the BEN-30 PR moves `benchmark/replay` to v2 in the same PR.
- Receiver used it for: `UNDERSTANDING_FAMILIES` amended, version 2026-11-03, fixtures and docs updated; the replay migration is queued for the moment #102 is on `main`.
- Evidence: this commit.
