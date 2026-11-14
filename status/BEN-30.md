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

### 2026-11-04 · Codex review of noema#112 (noemai-systems, on a033ba6), answered
- Finding (Major): `turns.sent` was incremented only after `start`/`message` returned, so a timeout after the request left undercounted the customer turns of an ERROR result.
- Done: the turn is counted when the request is sent, before any response (Codex option 1: turns mean requests issued). Test first, seen failing: a `start` that times out gives `turns == 1`, a follow-up that times out gives `turns == 2`. The `turns` description in `benchmark-case-result.v2` and the runner and scoring READMEs say so. A connection that was never established (core down) also counts the attempt; the metric is "issued", not "confirmed received" (option 2 would need the API wrapper to report transmission; not needed for REQ-BEN-009).

# Session 2026-11-13 · BEN-30 · scorecard v3 (ruling A190)

## Session summary
- What changed: `benchmark-scorecard.v3.json` (v2 plus the required `exact_upper95` on every rate: the exact one-sided 95% upper bound on a failure rate, Clopper-Pearson, `1 - 0.05^(1/n)` at zero failures, rounded up to six places, null on a rate that is not a failure rate or has no cases); `benchmark/scoring/rates.py` wraps the frozen `stats.make_rate`; the scorer, its diagnostics and the report use it; v3 fixtures; INVENTORY and contracts README migration notes.
- Why: A190 (interim, Founder-approved; committed on `main` by docs#169 in `DEC-2026-11-13-architect-rulings-a183-a192`, merged 14 Nov 2026): approve `exact_upper95` in scorecard v3, AGENT-15 steps, thresholds unchanged.
- Risks, and how they are contained: a new major for a strict reader of v2 (a required member is added). The v1 and v2 files are not edited and their bytes are pinned; a document is v2 or v3, never both. `stats.py` is hashed by the 27 Oct 2026 freeze, so it is not edited. No threshold, gate or margin is touched. `benchmark/publication` reads rate blocks by member name and keeps working; it does not check the new member. The qualification record copies only `k`, `n` and `rate`, so it is unaffected.
- Tests added (REQ-BEN-004): `test_rates.py` (12), `tools/contracts/test_benchmark_v3.py` (7), emission tests in `test_scorecard.py`, report tests in `test_report.py`; seven mutations of the new code, each caught.
- Regression cases added: none. Debt entries: none. Conflicts raised: none. (Codex's P1 on noema#540 was right that A190 had to be committed first, AGENTS section 2.3; docs#169 merged on 14 Nov 2026 and this note now cites the committed record.)

## Pre-work for the next agent
- Exact next step: if the Architect wants a published claim to carry the bound, `benchmark/publication/check.py` reads `exact_upper95` (a separate change; its rules are not changed here).
- Open questions (owner): none.
- Files to read first: `contracts/schemas/benchmark-scorecard.v3.json`, `benchmark/scoring/rates.py`.
- Do not touch: `benchmark/scoring/stats.py` (frozen by hash in `benchmark/decisions/2026-10-27-model-selection.md`).

