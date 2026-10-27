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
- Purpose: `status/ENV-08.md` (section of 22 Oct) in noema#57: remove the `needs_fault` markers for the five effects and drop `creates_case` once #57 merges.
- Receiver used it for: this change. The generator never wrote `creates_case`; a test now asserts the two effects with no params carry none.
- Evidence: noema#57; this PR.


# Session 2026-10-27 15:26 WAT · BEN-10

## Decision: the BEN-INJ-0003 proposal (noema#74) is closed without merging

The founder decided on noema#74. The pull request was closed without merging, after the comment below was posted on it. This section supersedes the "Open" item above that says `BEN-INJ-0003` waits for a decision on the move to `reason_code`.

> Closed, not merged. The proposal is not sufficiently validated to become the accepted benchmark behaviour. BEN-INJ-0003 stays marked needs_fault. Closing this proposal does not resolve the missing fault coverage. Revisit when the intended behaviour can be tested through the core adapter, in a fresh PR against main that links back to this one.

- **Marker and output:** the `needs_fault:INJECTION_PAYLOAD.get_case` marker on `BEN-INJ-0003` is unchanged. The generator output (version 1.3.0, the manifest digest), the case, its oracle and its fixture are unchanged. No case semantics changed.
- **The case remains unresolved.** Closing the proposal does not give the case a fault plan, so the fault coverage it needs is still missing. The runner still reports the case as blocked ("event: needs_fault:...").

## Session summary
- What changed (PR links): noema#74 closed without merging; this section is the only repository change.
- Why: the proposal was not sufficiently validated to become accepted benchmark behaviour.
- Risks, and how they are contained: none added. The case stays marked, so it cannot pass or fail on an injection that was not measured.
- Tests added (REQ, INV, US or EC IDs): none. The closed proposal's evidence test (`test_proposal_ben_inj_0003.py`) is not on `main`.
- Regression cases added: none.
- Debt entries: none.
- Conflicts raised: none.

## Why the proposal was deferred
The change moved the injection to a field that validates in the core adapter (`reason_code`, a case reason, PERD 27.2), so the test might measure that validation and not free-text handling. The proposal's own list of what its evidence did not show named the same risk: the injected text is appended after a real code, and if core rejects the value at the adapter, the case would exercise the rejection and not the handling of untrusted text at the model boundary.

## Evidence missing
- No observed outcome through the core adapter. The proposal compared generated documents and loaded the plan into a real mock-case; it did not run the case through core, which does not serve the investigation path.
- No proof that the injected payload reaches the intended processing boundary.

## Revisit criteria
The case is reopened only when a test shows all of these:
1. the injected payload reaches the intended processing boundary;
2. it exercises the attack surface the benchmark claims to measure;
3. the test can tell a successful injection-path result apart from an early schema or adapter rejection.

A reopened proposal is a fresh pull request against `main` that links back to noema#74.

## Messages

### 2026-10-27 (time not recorded) accepted · Founder (relayed by the user) → Session C
- Purpose: the founder decided on #74. Close it without merging, after posting a stated comment on it; record the decision in a new dated section of this file (why deferred, evidence missing, marker unchanged and case unresolved, revisit criteria); log every message in this file, this instruction included; make no other change; merge nothing; report the closed pull request's link and the commit that adds the section, with its checks. The marker, the generator output and any case semantics stay as they are.
- Receiver used it for: closing #74 and this section.
- Evidence: noema#74; this pull request.

### 2026-10-27 (time not recorded) accepted · Founder (relayed by the user) → Session C
- Purpose: the earlier instruction of 27 Oct that asked for #74: a pull request proposing the move from `case_note` to `reason_code`, with evidence that the injection test, attack surface, fixture meaning and expected outcome are unchanged, and the marker kept until approval. It is logged in full in `status/BEN-03.md`; its entry here lived only on #74's branch and is recorded again because #74 closed unmerged.
- Receiver used it for: #74 (now closed): `PROPOSAL-BEN-INJ-0003.md`, `test_proposal_ben_inj_0003.py`. Its evidence found that `get_case` cannot carry the per-transaction match in mock-case, so the proposal named `find_cases` as well as `reason_code`.
- Evidence: noema#74 (closed).

### 2026-10-27 (time not recorded) sent · Session C → Session A (pull request comment on #70)
- Purpose: reported the file overlap between #74 and #70 (`status/BEN-10.md`); no conflict.
- Receiver used it for: not recorded. The overlap ended with the closing of #74.
- Evidence: comment on noema#70.

### 2026-10-27 (time not recorded) sent · Session C → readers of #74 (pull request comment)
- Purpose: the comment quoted above, posted before closing.
- Receiver used it for: not recorded.
- Evidence: comment on noema#74.

## Pre-work for the next agent
- Exact next step: none for this case until a test meets the revisit criteria.
- Open questions (owner): which core adapter test can separate a successful injection-path result from an early rejection (benchmark owner and Session A, who owns the case adapter).
- Files to read first: this section; `benchmark/generator/lib_security.py` (`inj_case_note`); `synthetic/mock_case/internal/api/pipeline.go` (`injectText`); the closed noema#74.
- Do not touch: the `BEN-INJ-0003` marker, the generator output and the case semantics, until the criteria are met.
