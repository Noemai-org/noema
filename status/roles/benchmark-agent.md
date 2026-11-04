# Status · benchmark-agent

Written only by the benchmark-agent role (see `README.md`). Append dated entries, newest last. History before 31 Oct 2026 is in `status/<task-id>.md`.

## Entries

(Moved here on 4 Nov 2026 from `status/roles/benchmark-owner.md`, a file name this role used before `status/roles/README.md` named it `benchmark-agent.md`. The entries are unchanged; dated sections follow.)

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

## 2026-11-04 · Status

**Completed:** noema#102 (BEN-05 replay tool) merged, with its authority docs#104 (DEC-2026-11-04, the named open exclusion). Status file renamed to this one.
**In progress:** replay v2 migration into noema#112; the G1 stand-in data contract (noema#161) and the labelled G1 evidence (noema#117) await review and merge.
**Decisions taken:** Founder, 4 Nov: G1 uses a Noema-side stand-in (Option B), evidence labelled "stand-in", rerun against live Tushe once its OpenAPI is frozen; unpause of the core's Tushe-dependent adapters approved for the stand-in only; `monetary_calls` source is a per-route request log in the hostility proxy (Integration Lead builds it); no `tushe-admin` job; secret names `TUSHE_RUNTIME_KEY`, `TUSHE_FIXTURE_KEY`, `TUSHE_ADMIN_TOKEN`.
**Blocked:** the G1 run needs the stand-in service, the proxy log and session tokens (other roles).
