# Status · BEN-10 (Session C) · generator fault plans

Updated 2026-10-24. Owner: Session C. Related tasks: BEN-03 (runner), ENV-08 (mock-case effects, Session B), TST-02.

## State

- Generator 1.3.0: the five mock-case effects added by noema#57 (`NEAR_TIMEOUT_BEFORE`, `NEAR_TIMEOUT_AFTER`, `HTTP_200_SEMANTIC_PENDING`, `IDEMPOTENCY_COLLISION`, `INJECTION_PAYLOAD`) are emitted as `fault-plan.v1` faults. `needs_fault:` markers: 11 before, 1 after (`BEN-INJ-0003`: it injects `case_note` of `get_case`; the contract allows `reason_code` there).
- Runner support on the default suite: 143 of 320 cases can run (was 134).
- `benchmark/generator/catalogue.py` cites PERD v1.15 at docs `67a60ec`; the 105 EC, 22 US and 8 PUS IDs were compared with `NOEMA_POC_PERD_v1.15.md` and match.
- Provider cost stays `null`, "not available", in every result.

## Open

- `BEN-INJ-0003` needs either a contract change (`case_note` as an injectable field) or a founder decision to move the injection to `reason_code`; neither is made here.
- Cost: the founder's route is that the model service writes `$(BENCH_OUT)/cost/<call_id>.json`. The reading side waits until that writer is on `main`. Session A's status (`OPS-07.md`) records that nothing wires `BENCH_OUT` yet.
- Bake-off: not run, no access (no `decide()` on `main`, no `OPENAI_API_KEY`).
- noema#55 (Session C) is superseded by #56; closing it is a founder action.

## Messages

### 2026-10-22 13:36Z accepted · Session D → Session C
- Purpose: noema#43 merged (`6013461`); the harness `CallRecord` fields (`cost` in micro-units, `complete`, per-attempt `cost_micro`, null means unknown) are on `main`.
- Receiver used it for: checked that the record exists on `main` and that no public path serves it; reported that cost stays null until a route is decided. No code change.
- Evidence: noema#43.

### 2026-10-22 (session record, no time kept) accepted · Session B → Session C
- Purpose: `docs/sessions/2026-10-22-1530-ENV-08b.md` in noema#57: remove the `needs_fault` markers for the five effects and drop `creates_case` once #57 merges.
- Receiver used it for: this change. The generator never wrote `creates_case`; a test now asserts the two effects with no params carry none.
- Evidence: noema#57; this PR.

### 2026-10-27 (time not recorded) accepted · Founder (relayed by the user) → Session C
- Purpose: a PR that proposes moving the `BEN-INJ-0003` injection from `case_note` to `reason_code`, with evidence that the injection test, attack surface, fixture meaning and expected benchmark outcome are unchanged; the marker stays until the benchmark owner or the non-author co-founder approves. (The cost-file part of the same instruction is logged in `status/BEN-03.md`.)
- Receiver used it for: `benchmark/generator/PROPOSAL-BEN-INJ-0003.md` and `test_proposal_ben_inj_0003.py`. The evidence found that `get_case` cannot carry the per-transaction match in mock-case, so the proposal names `find_cases` as well as `reason_code`.
- Evidence: the proposal pull request.
