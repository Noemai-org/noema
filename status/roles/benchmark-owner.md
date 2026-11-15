# Role status · Benchmark Owner agent

Only this role writes this file. Append dated entries; never rewrite history. Limits: non-held-out work only; no access to `benchmark-heldout`; no core build areas; no live model calls or `bench-live` dispatch without founder approval (cap $25, about $0.33 spent, none added here).

## 2026-10-31 · Assessment of BEN-01 to BEN-05 against `main` (34f6a8b)

Evidence is from local runs on `main` plus the files named. A task's `done_when` is checked, not its `current_status` (tasks.csv shows `Unassessed`, OPS-09 is the Repo Steward's). There is no check-run link for these local runs; they are information, not acceptance.

| Task | done_when | What I observed | Assessment |
|---|---|---|---|
| BEN-01 | Schema validates a sample case; manifest records generator version and seeds | `contracts/schemas/benchmark-case.v1.json` and `benchmark-suite-manifest.v1.json` exist. All 320 generated cases validate (the generator and runner validate on load). The manifest records generator name, version, seed and each case's digest. | Met in substance |
| BEN-02 | US-01 and US-06 cases validate; each fixture names its fault source | Cases covering US-01 (`BEN-CLEAN-0001`) and US-06 (`BEN-TMO-0001`) have fixtures, oracles and fault plans (`fp_none`, `fp_ben_tmo_0001`); plans are `fault-plan.v1` with a `source`. | Met in substance |
| BEN-03 | Runner executes US-01 against stubs end to end | `benchmark/runner` drives the public API; its tests run a stand-in public API (`FakeNoema`) end to end. Cost reader is validated against hand-written records only, never the real producer (status/BEN-03.md). | Met against stubs. Not run against a real core: none serves the investigation path |
| BEN-04 | 50 cases validate; two runs give identical results; the runtime cannot read any oracle field | 320 cases validate; two generations are byte-identical (diff -r, seed 20261020). Oracle isolation is by construction (separate credential, counters in the result) and unit-tested; it is observed only live (`oracle_accesses_by_runtime`), which no run has done. I did not check the named adversarial fixtures one by one: coverage is reported by tag (105 of 105 EC, 22 of 22 US, 29 of 29 faults, 20 of 20 capabilities). | Partly evidenced. "Two runs identical" is shown for generation, not for execution. 177 of 320 cases are blocked from running by unwired faults and events (143 can run); `BEN-INJ-0003` stays `needs_fault` by founder decision (noema#74 closed) |
| BEN-05 | Identical → G1 evidence | Tooling built this session (`benchmark/replay`). The run waits for OUT-01 and REC-03, neither on `main`. | Not done. Prepared. See `status/BEN-05.md` |

Check results (local, `main` plus this branch): `make bench-test` 513 tests on `main`, 545 with the replay tests, `mypy --strict` clean.

### Decisions taken (inside scope, reversible)
- The replay report carries no new contract in `/contracts`; it is a benchmark output documented in `benchmark/replay/README.md`. If it becomes an input to a gate, a contract with a version is the next step (AGENT-15).
- The diff treats errored or missing cases as NOT_COMPARABLE, never identical.

### Escalation 1 · which cases are the BEN-05 evidence (charter §6, item 4)
Problem: six cases cover US-01 and US-06; `BEN-CLEAN-1007` needs a Tushe-held injection fault, so an all-six run cannot reach IDENTICAL. Options, effort, risk and recommendation are in `status/BEN-05.md`. Recommended: compare the five base cases (`--exclude-mutated`), recorded in the report. Question: do the founders accept B?

### Next
BEN-05 waits for OUT-01 and REC-03. Next work, in the order DEC-2026-10-31 sets (it wins where it differs from the role prompt's "Start here"): BEN-30, BEN-29 (non-held-out parts), TST-06, UND-13, BEN-31; then the G2 case families BEN-06 to BEN-09 and BEN-12 in tasks.csv order.

## 2026-11-01 · Escalation 1 decided
The founders chose option B for BEN-05: the five runnable base cases (`--exclude-mutated`), with `BEN-CLEAN-1007` a named, open exclusion tied to the Tushe-held `INJECTION_PAYLOAD` fault. The report now states "covers 5 of 6 cases" and lists open exclusions (noema#102). Next: overlap check and check table on #102, then BEN-30.

## 2026-11-06 · Work since 5 Nov, evidence for the Steward's assessment, and blockers
Claims, not evidence, until merged (AGENTS §9). Each item names its pull request; check-run links are in that pull request's description at its final head.

**Merged:** noema#161 (BEN-05 stand-in data contract and loader), #138 (BEN-29 dispatch workflow), #166 (BEN-18 replay `--all`), #204 (the `compose` profile check knows the stand-in services; main's `compose` workflow was red after #169).

**Open, with Codex requested once at the final head:** #124 (BEN-31 publication check), #117 (BEN-29 one command per suite), #170 (BEN-05 `monetary_calls` from the proxy request log; blocked on the Integration Lead's per-start `generation`, ruling A22), #199 (UND-13: `qualification-run.v1` and `qualification-record.v1` contracts, the subset, baseline, comparison, lookup and CLI, ruling A17), #235 (ruling A15: benchmark roles are the four identity roles; generator 1.4.0). docs#121 files DEBT-17 (qualification records and the baseline are unsigned JSON in git). docs#125 lists every mismatch between Comparison Arm Brief v1.0 and the real pack and environment, for decision; nothing was changed.

**Generated-layer coverage (BEN-06 to BEN-09, BEN-13, BEN-15; BEN-10, BEN-14, BEN-16 partly).** `python -m benchmark.generator coverage` on `main` at `9d00e34`: 320 cases, **0 gaps**; US 22 of 22, PUS 8 of 8, EC 105 of 105, fault families 29 of 29, capabilities 20 of 20. The `Quotas` and Pidgin-share tests in `benchmark/generator/test_generate.py` pass in CI. This is what the "Cases validate; each is linked to its EC or US ID" and "Coverage report shows no empty cell" parts of those rows ask for on the generated layer. **Not covered by this evidence, so those rows are not Done:** the first full scored run by family (BEN-16, BEN-12) needs a running Noema; the hidden-oracle share and the held-out cases (BEN-11, BEN-19, BEN-28) are the Founder's, in the private repository, and I do not touch them. `tasks.csv` still shows BEN-06 to BEN-09, BEN-13 and BEN-15 as In progress; the Steward decides what the evidence supports.

**Blocked on a human or another role:**
- BEN-29 dispatch of `bench-suites`: HTTP 403 for my credential (no Actions write permission on `Noemai-org/noema`). A human dispatches `us`, `chaos`, `evil`, `full`, `g1` from the Actions tab, or grants the permission. I do not authenticate as the other account.
- BEN-05 G1 run: the core's stand-in adapter (Integration Lead, A10) and the proxy `generation` (A22). #169 is merged.
- UND-13: the Founder-set pass^k floor. Until it exists and a baseline record exists, every first qualification is NOT_QUALIFIED. Decision record text was posted on docs#117.
- #235: the Pack owner moving `packs/fintech_v1.json` from `ops_agent` to `ops` (A15).
- TST-06: waits for C2's canary.
- Comparison arm work: no arm starts before G3 (SoT §38.5); the Brief's issue date, the arm-facing tool facade and four other decisions are in docs#125.

**Note on tools:** a Python 3.15 interpreter and a locked-dependency virtual environment were installed on this machine on 6 Nov, so tests are now run locally before each push; CI at the final head remains the evidence.
