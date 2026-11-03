# Status · integration-lead

Written only by the integration-lead role (see `README.md`). Append dated entries, newest last. History before 31 Oct 2026 is in `status/<task-id>.md`.

## Entries

### 2026-11-03 (end-of-day report)

**Completed** (a claim counts only with a check-run link at the final head)
- Main-red fix, ENV-08 follow-up and TEN-03, TEN-04: [#103](https://github.com/Noemai-org/noema/pull/103), merged (`9c6a472`). Mock-case after-commit effects commit even when the caller gives up first; three tests poll instead of counting at once; regression case `TestAfterCommitEffectsCommitWhenTheCallerGivesUpBeforeTheCommit`. CI at `25a5dbe`: [mock-case run 41239464227](https://github.com/Noemai-org/noema/actions/runs/41239464227). Decision of the founders: one green run at the final head plus 80 local starved runs (0 failures; the original test failed 3 of 60).
- TEN-01: [#116](https://github.com/Noemai-org/noema/pull/116), merged. Compose job at `1c808e0`: [job 125742359892](https://github.com/Noemai-org/noema/actions/runs/41583146572/job/125742359892), line `TEN_RESULT id=TEN-01 seed=none pass=true`. The same log is the ENV-08 Compose-path evidence (mock-case truth endpoint: bench credential 200, runtime credential 403, none or guessed 401).

**In progress** (all green at their final heads; none has a Codex review yet except where stated)
- ENV-15, [#115](https://github.com/Noemai-org/noema/pull/115) draft, head `e96c9a7`: `fault-plan.v2.json` beside v1 (v1 bytes pinned), `INVALID_JSON`, `WRONG_CONTENT_TYPE`, `EMPTY_BODY_SUCCESS`, proxy `PAGINATE`; the case adapter now reads a 2xx body only under `application/json` (found by the 18 recorded corrupted exchanges: all three `wrong_content_type` ones were observed as a fact or an acknowledged write). [Check table](https://github.com/Noemai-org/noema/pull/115#issuecomment-6596129004). The generator and runner move to v2 waits for BEN-30.
- TEN-02, Trust half, [#119](https://github.com/Noemai-org/noema/pull/119) draft, head `416eec2`: 64 seeded concurrent replays equal the first pass and the committed `decisions.json` (seed 20261103). **The verdict half is not built** (OUT-01); TEN-02 is not passed. A reviewer marked it NOT READY for that reason; I agreed and it stays a draft.
- AT-14 recorded run, [#125](https://github.com/Noemai-org/noema/pull/125) draft, head `8d3d670`: [check table](https://github.com/Noemai-org/noema/pull/125#issuecomment-6602396264).
- AT-04, AT-05, AT-07 recorded runs, [#126](https://github.com/Noemai-org/noema/pull/126) draft, head `1eee933`: [check table](https://github.com/Noemai-org/noema/pull/126#issuecomment-6602448302). One production line: the proxy's trace for `DROP_WEBHOOK` names the dropped index.

**Blocked**
- TST-05 (crash-point matrix): needs EXE-02, OUT-01, RCV-01; `outcome/` and `execution/recovery/` are `doc.go` only on `main`. The controller already has 14 crash points and a simulated crash matrix (`execution/controller/crash_test.go`); what is missing is the same matrix on real PostgreSQL, the real mock-case and a real FaultPlan, asserted on mock-case's truth endpoint. That needs a harness that calls the control and truth endpoints from core test code, which `execution/e2e/README.md` forbids today. See Escalations.
- TEN-02 verdict half: OUT-01 (Core Engineer 2).
- TEN-05 (Tushe part), TEN-07, TEN-08: G0-T. The Tushe joint session outcome has not reached this role yet; nothing is inferred about Tushe.
- ENV-14 as a whole: CON-03 (Core Engineer 1) is not on `main`, so `codegen-diff` is skipped in every run; ENV-05 and ENV-13 are partial (relay endpoint registration and pull client depend on the Tushe outcome); the `/contracts` v1 tag is a founder action. G0-T has no date from Tushe.

**Decisions taken** (each recorded in its PR body)
- ENV-15: `fault-plan.v2.json` beside v1, not an in-place relaxation of v1; only three genuinely new effects (truncated body, and extra or missing field, already exist as `MALFORMED_RESPONSE` and `SCHEMA_DRIFT`).
- "Observed by the adapter": a test-only route table fronts mock-case under a placeholder target; the corrupted exchanges are recorded from the real proxy with a drift guard.
- AT records live in `synthetic/at_runs/`, not in the Tushe folder; what Tushe is shown is the founders' call.
- TEN-01's "oracle" is the stack's stand-ins; Tushe's own oracle is not claimed.

**Escalations**
1. TST-05 harness boundary: (1) a narrowly scoped test-only package under `services/core/execution/` that holds the bench credential, with a recorded exception to the e2e README rule and Security Reviewer sign-off; (2) the harness in the benchmark runner driving a compiled core test binary; (3) sim tests only. Recommendation: option 1. Awaiting the Architect.
2. The old remote branch `claude/quirky-faraday-b5o2ni-env14` holds the same commits as #115 and has no PR; delete after #115 merges.
3. Codex has not reviewed #115, #125 or #126 (it reviewed #116 and #119 within about 20 minutes). Standing order 7 marks a PR Ready when green and reviewed; asking whether to mark them Ready now.

**Next task**
- TST-05 design note and exactly-one-effect kill-point list (starts on the Architect's answer to Escalation 1); TEN-06 the moment #115 merges (needs `PAGINATE`); TST-06 chaos subset with the Benchmark agent after the TST-05 design.
