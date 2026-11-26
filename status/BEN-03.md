# Status · BEN-03 (Session C) · runner, provider cost

Updated 2026-10-25. Owner: Session C. Related tasks: BEN-10 (generator, `status/BEN-10.md`), UND-02 (Session D, cost files), BEN-12 (scorer).

## State

- The runner reads provider cost from the model service's bench cost files (`--cost-dir`; `make bench` passes `$(BENCH_OUT)/cost`), layout `<correlation_id>-<NNN>.json`, each the JSON of `CallRecord.to_dict()`. `benchmark/runner/cost.py`; tests in `test_cost.py` and `test_run.py`.
- A case's `cost_usd_micros` is the sum of its calls' `cost.total_micro`. It is null ("not available") when there is no directory, no file, or any file is unreadable, incomplete, not an integer or not USD. Never estimated, never converted, never read from the reconstruction.
- The scorer and report are unchanged in behaviour (null in, null out); wording no longer says "no source exists".

## Open

- Not read end to end: the model service writes no file until its request path constructs the harness (`status/UND-02.md`). The tests use records written by hand to the service's shape.
- Currency: `cost_usd_micros` is millionths of a US dollar. A provider rate card in another currency makes the case null. If the founders want a conversion, that is a decision for the spec first (rate source and date); none is invented here.
- `BEN-INJ-0003` keeps its `needs_fault` marker. A separate proposal PR covers the move to `reason_code`; the marker changes only after the benchmark owner or the non-author co-founder approves it.
- Bake-off: not run, no access.

## Messages

### 2026-10-25 (time not recorded) accepted · Founder (relayed by the user) → Session C
- Purpose: the cost file name is `<correlation_id>-<NNN>.json`; the cost reader is to start against that layout; `BEN-INJ-0003` was not to change yet; the PR link and check results with run links on the final head were to be reported; every message is logged in `status/`; the PR is not merged by Session C.
- Receiver used it for: this change (`cost.py`, `--cost-dir`, tests, READMEs).
- Evidence: this PR.

### 2026-10-24 (before this branch) accepted · Founder (relayed by the user) → Session C
- Purpose: the cost route: the model service writes each call's cost to `$(BENCH_OUT)/cost/<call_id>.json` in the `CallRecord.to_dict()` form; the reader reads it from there; `services/**` is not imported and no public endpoint is added; cost is reported as null until the file exists on `main`.
- Receiver used it for: the design of `cost.py` (a directory read, no import, no endpoint). The file name was then settled by the instruction above.
- Evidence: noema#62 (the writer wiring), this PR.

### 2026-10-26 14:28Z accepted · Session A → Session C
- Purpose: relays a founder instruction of 26 Oct to Session A to keep instruction or prompt text out of PR titles, bodies and status files; Session A found nothing to remove in the title and body of #64 and said two lines of this file carry imperative wording.
- Receiver used it for: this file now states what was received and done, without imperative wording; applied in this PR.
- Evidence: this PR.

### 2026-10-27 (time not recorded) accepted · Founder (relayed by the user) → Session C
- Purpose: decisions are final. (1) `BEN-INJ-0003`: a PR proposing the move from `case_note` to `reason_code`, with evidence that the injection test, attack surface, fixture meaning and expected outcome are unchanged; the marker stays until the benchmark owner or the non-author co-founder approves. (2) Cost file naming: canonical name `<correlation_id>-<NNN>.json`; a correlation id with a character outside `[A-Za-z0-9_-]` is rejected and not normalised; NNN is 001 to 999 and the 1000th call is refused; files are created exclusively; the same rules in producer and reader; tests for multiple calls, colliding ids, call 1000 and no overwrite. One PR for item 2, with test results.
- Receiver used it for: this PR (producer `CostFileTelemetry`, reader `cost.py`, tests, READMEs). The producer is in `services/model`, which Session D owns; this instruction is the reason it is changed here.
- Evidence: this PR.

### 2026-10-27 (time not recorded) accepted · Founder (relayed by the user) → Session C
- Purpose: #64 failed because its branch predated the scan-scope fix (#69); `origin/main` is to be merged into `sC/t4-cost-reader` without rebase or force-push, the result pushed, checks run once on the new head, and the head SHA and each check's result reported in one table with run links on #64; the founder merges after the checks pass; every message is logged in `status/`.
- Receiver used it for: the merge of `origin/main` into `sC/t4-cost-reader` (no conflicts), done in #64, and this entry.
- Evidence: #64.

# Session 2026-11-16 · BEN-03 · the plan for the harness's Tushe read-only fold and trace join

## Session summary
- What changed: `benchmark/oracle_client/PLAN-tushe-readonly-fold.md` and a pointer in `benchmark/oracle_client/README.md`. A plan only.
- Why: the Founder's instruction of 16 Nov 2026: plan the harness oracle fold for transitions (Tushe M-02) and the traceId join (AT-19), to read through the read-only key when the Founder creates it (A240).
- Risks, and how they are contained: none in code. The plan refuses to build a reader or name a route before a delta file adopts the addendum (DEC-2026-10-16 E10; the Steward's record of the addendum), and lists what is not known instead of guessing it.
- Tests added: none (a plan; the tests it will start with are listed in its section 4). Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: after the Founders adopt the addendum's routes by a delta file, build `tushe_fold.py` and `tushe_trace.py` (pure, tests first), then the reader.
- Open questions (owner): the six in section 5 of the plan (routes and shapes, the order field, the join key, the scopes needed, time, the reply of 10 Nov): the Steward, the Founders and Tushe.
- Files to read first: `benchmark/oracle_client/PLAN-tushe-readonly-fold.md`, `benchmark/oracle_client/README.md`.
- Do not touch: no credential value is ever written in a file; the key goes in the bench Environment only.

# Session 2026-11-18 (second round) · BEN-03 · Codex P2 on the fold at 52fbc856

## Session summary
- What changed: `fold_transitions` reads every row first (an unreadable row still fails the whole answer), then applies the one-id-two-rows rule against the cutoff: if every differing row is after the cutoff the id is not part of the answer (the prefix property, PLAN-tushe-readonly-fold.md 3.2); if any is at or before it the in-prefix row is the contested one and the answer is CHAIN_BROKEN.
- Tests: 3 new, written first and failing before the fix; 8 mutations of the rule, 7 caught; the eighth (keeping a future-only conflicting id) is an equivalent mutant, since such a row never enters the prefix.
- Regression cases: none. Debt: none. Conflicts: none.

## Pre-work for the next agent
- Exact next step: after Codex re-reads the fix lines (A287), the independent read, then one Founder approval. The reader waits for noema#711, the delta file and the read-only key.
- Do not touch: the row shape defaults (assumed until the delta file adopts them).


# Session 2026-11-25 · BEN-03 · A667: the timeline drivers for two webhooks and a resume

## Session summary
- What changed: `benchmark/runner/drivers.py`, `env.py` (a `WebhookSender` port and `RunEnv.webhooks`), `run.py`, `support.py`, the runner README, and `test_event_drivers.py` (15 tests).
- Driven now: `session_resume:same_channel` (the principal's token is asked for again and the later turns go to the same conversation with it; no clock move: offsets order events and are not waited for, as before) and `webhook:settlement_updated` and `webhook:reversal_posted` (only when the environment has a `WebhookSender`; each is delivered once, in timeline order, after the first input has settled). Every other kind still makes the case an ERROR (`EVENT_KIND_UNSUPPORTED`) and Noema is not called. A sender failure is `WEBHOOK_DELIVERY_FAILED` (the class of the failure, never its text). An event at the start of the timeline, where only a kill switch and a clock advance are driven, was silently dropped before; it is now `EVENT_AT_START_UNSUPPORTED`, refused before any side effect. None of the 320 generated cases has one.
- **No transport is built, on purpose.** The environment has no interface that delivers a webhook to the core: the relay accepts only Tushe's signed deliveries (its signing secret and Tushe's payload shape are not shown to the runner), and mock-case's control API has reset, fault plan and clock only. Inventing a control endpoint or a payload would be a Tushe behaviour that has not been shown (AGENTS §14). So the runner has the driver and the port, and no sender: a webhook case stays an ERROR until the environment owner supplies one.
- Effect on the suite (`python -m benchmark.runner --support`): 145 of 320 can run (143 before): the two resume cases (`BEN-SESS-0001`, `BEN-SESS-0007`; they are J8's cases). The 15 webhook cases stay blocked by faults and events no driver has (`DELAYED_REVERSAL` 8, `DELAYED_SETTLEMENT` 4, `LATE_EVENT` 3, `watch_deadline` 3, `process_restart` 2, `listener_down` 1), even with a sender.
- Risks, and how they are contained: a resume that obtains the same cached token (the IdP provider reuses a token within its life) exercises "the same principal returns to the same conversation", not "a new session"; the test pins what is shown. The change makes no case more lenient: the only new passes are cases whose events are now driven.
- Tests added (REQ-BEN-002, AGENT-12, A667): 15, seen failing first (5 failures, 6 errors). Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: A682 settles the transport: IL adds a mock-case control endpoint that delivers webhooks through the relay path, signed with mock-case's own SYNTHETIC key. When it exists, the runner gets a sender that calls it (one small PR: a `MockCaseControl` method and the flag); until then a webhook case stays an ERROR.
- Open questions (Architect): (1) the interface: a control endpoint on mock-case, or a signed delivery to the relay (then the runner holds the signing secret: SR); (2) settled by A682: a resume does not move the simulated clock (a test pins it).
- Do not touch: the held-out set and the oracle (INV-22).

# Session 2026-11-25 · BEN-03 · A689: `--support` applies the start-position check

## Session summary
- What changed: `support.reasons_for` now adds `event at start: <kind>` for an event the run refuses at the start of the timeline (`EVENT_AT_START_UNSUPPORTED`), so `--support` says what a run would do. The runner README's count is recomputed with the CLI: **145 of 320 can run** (175 blocked; 119 need no fault and no event), unchanged: 26 cases carry an event at the start and every one was already blocked by another reason, so the fix changes the reasons table, not the count. With a webhook sender the count is still 145: all 15 cases that hold a driven webhook kind are also blocked by a fault or an event no driver has.
- Test-first, seen failing in CI: the red commit `41c0b6af` (tests only, 3 failing by design) was pushed before the fix. Tests: the report blocks a confirm at the start and names it, blocks a driven kind at the start even with a sender, does not block a kill switch or a clock advance at the start, blocks by name every suite case the run refuses at the start, and the README count equals the one the report computes.
- Risks, and how they are contained: none for the run; the report only gets stricter. Tests added (A667, A689): 4 new, plus 1 guard. Regression cases added: none. Debt entries: none. Conflicts raised: none.


# Session 2026-11-26 · BEN-03 · A667, A682: the runner's WebhookSender for mock-case

## Session summary
- What changed: `benchmark/runner/webhooks.py` (`MockCaseWebhooks`, `read_payloads`), two methods on `MockCaseControl`, the `--webhook-payloads` flag (it also feeds `--support`), the runner README. Built on #979's port; it goes in its own PR after #979 merges.
- Why: A682 and IL's endpoints (`POST /control/v1/webhooks/deliver`, `POST /control/v1/webhooks/{message_id}/replay`; status/ENV-05.md) give #979's `WebhookSender` a real transport.
- Risks, and how they are contained: the object delivered for each kind is the caller's file, because no governing document states Tushe's body members; nothing is invented here. A relay that does not accept is an ERROR, never an OK.
- Tests added (A667, A682): 9 in `test_webhooks.py`, on a real TCP server. Seen failing first (the red commit has no module).
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: IL says which members the relay's consumer needs in each body, so a payload file can be written; the runner then needs no change.
- Do not touch: the mock-case routes.
