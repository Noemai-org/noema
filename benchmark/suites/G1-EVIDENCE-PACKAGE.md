# G1 evidence package: the template and the dispatch preconditions

**Task:** BEN-05 (G1 evidence run, replay diff). **Owner:** Benchmark Owner role agent. **Gate:** G1, the unknown-outcome slice (plan 3.1; PERD 69; gate record `docs/poc/execution/gates/G1.md` in the `docs` repository). **Written:** 14 Nov 2026.

This file is a **template**. It holds no result. The package itself is one document written at the end of the run (section 4), from links the Benchmark Owner opened; a row is never marked MET from a description. The criteria are the Security Reviewer's, posted on noema#436 on 14 Nov 2026 (items 1 to 12). Where this file and that comment differ, the comment is right and this file is fixed.

## 1. What counts, and what does not

G1 needs all four conditions of the gate record, shown, none inferred: US-01 and US-06 run end to end on text through the public API; the unknown outcome verified by key and never retried; the Record reconstructs both; the property tests for INV-03, INV-07, INV-08 and the prohibited registry green. Plus the security conditions (items 5 to 12 below): any one failing fails G1.

**Does not count:** anything before G0 passes (both halves); a run on a head that is not the final head; a retried run presented as the first; a partial run; a "not run" presented as a pass; component-level e2e tests in place of the public-API run (they stay in the package as CI evidence); any claim stronger than "tamper-evident". **The data label stays:** enterprise data is the stand-in (to be rerun against live Tushe), and the model side is the replay provider (no live call, no provider key). Both are written on separate lines in the package.

## 2. The twelve items: what closes each, who produces it, how the Benchmark Owner checks it

| # | Item | Evidence that closes it | Produced by | How BO checks (a link BO opened) |
|---|---|---|---|---|
| 1 | US-01 and US-06 end to end on text through the public API | The runner's request log shows calls to the real `/v1` edge with synthetic IdP session tokens (`iss` `noema-poc-idp`, one role, at most 15 minutes); the case runs through `RunWrite` and the real controller over Postgres and mock-case; arch-lint green | IL (stack workflow), EL (`RunWrite`, composition) | request log in the run artifact; `gh pr checks` at the final head |
| 2 | The unknown outcome is verified by key and never retried | For US-06: exactly one `GRANT_SPENT`, one `DISPATCH_ATTEMPTED`, one `RECEIPT` classified `OUTCOME_UNKNOWN`, then a verification read by the idempotency key and a verdict; the mock-case log shows exactly one write for that key; no second grant; the same counts after a recovery sweep and after a replay. A second dispatch anywhere is an automatic fail | EL, C1, C2 (verifier); BO counts | Record reconstruction plus the mock-case log, counted by BO |
| 3 | The Record reconstructs both cases | The reconstruction of both correlations served through the audit route by an `audit` session of the same tenant; chain verified from genesis; one `RECORD_ACCESSED` log line per read; the neutral export (REC-09) digest recomputed independently; no step missing; every move recorded before the next action (EC-E16) | C1 | reconstruction files in the artifact |
| 4 | Property tests green at the final head | Name each test and link the check run: INV-03 `TestINV03NoDispatchWithoutAValidUnexpiredUnusedMatchingGrant`, `TestINV03EveryDefectUnderEveryFault`; INV-07 `TestINV07ATimeoutAFiveXXOrSilenceNeverMapsToSuccessOrFailure`, `TestINV07EveryFaultAndEveryCrashPoint`; INV-08 `TestINV08EveryWriteCarriesAKeyCommittedBeforeTheCall`, `TestINV08EveryCrashPointUnderEveryFault`; prohibited registry `TestProhibitedIDsAreNeverCallable` (names from C1's list on noema#436; re-read at the final head) | C1 | the `core` job log at the final head |
| 5 | One final head | Every link is to the same final head SHA (the dispatch SHA); a push after the run voids it. Run ID, seed and FaultPlan ID recorded. Check runs read at that head, never inferred | BO | `git ls-remote` and `gh pr checks` at that SHA |
| 6 | The fault is a hostility-proxy FaultPlan, not a mock | The plan and its seed are in the evidence (AGENT-12, REQ-TST-004) | IL, BO | the plan file in the artifact |
| 7 | INV-22 holds and is shown | The mock-case log shows zero accesses to oracle or held-out routes by the core or the model service; the log collector was alive the whole run (a dead collector makes the run NOT RUN, never zero); the resolved compose config shows the model service mounting only `cost/`; no oracle-side file in any uploaded artifact | IL, BO | mock-case log, resolved compose config, artifact file list |
| 8 | Leak scan over every artifact | `tools/ci/leakscan.py` over logs, traces, API responses, the Record dump, explanation text, the step summary and the uploaded files: zero findings, and no digit run longer than 4 (A154, A184). **One planted canary the scan must find**, so a scanner that finds nothing is not read as clean | IL (step), BO | scan output including the canary finding |
| 9 | Identity and tenant are exercised | One negative probe per case: a token for another tenant and a token with a wrong role are refused with the not-found shape and the handler never runs; the principal and tenant in the Record equal the token's | IL, EL | probe results and the Record |
| 10 | The result is scored | The scorer runs on the results; Tier 0 all zero; "NOT RUN" or an incomplete run counts as a fail | BO | the scorecard (v3 once noema#540 merges) |
| 11 | Two clean legs and a replay diff (BEN-05) | The two legs of each case decide identically and the replay report shows no difference in decisions and verdicts | BO (`python -m benchmark.suites g1`) | `replay/report` in the artifact: IDENTICAL |
| 12 | No secret or key in any artifact or summary | The synthetic issuer's throw-away key stays in a `0600` file outside the uploaded set; nothing in the repository, a log or a comment carries a credential | IL, BO | artifact file list; leak scan |

## 3. Dispatch preconditions: when BO posts READY TO DISPATCH

BO posts **READY TO DISPATCH at <sha>** on noema#436 (and noema#117) only when **all** of these are merged on `main` at that SHA and every check run at that SHA is green, read by BO with `gh pr checks`:

1. The stack workflow (noema#444) and IL's follow-up that passes the four `--idp-*` flags and a throw-away key pair.
2. The Trust lane: C1's assembly of T1 to T15 and EL's `Trust.Decide` adapter (A186 lookup included), recording and grants in one transaction.
3. The write lane: `RunWrite`, the Executor and Verifier ports and their adapters (EL), with the worker registered in `cmd/core`.
4. The read lane: the compile route (noema#544), the `Interpreter` adapter (noema#519), the Evidence adapter (C2) and the reconciler, `POST /v1/conversations` and the investigation and audit routes mounted, and `/readyz` ready.
5. The steps SR's items 6, 8 and 9 need, in the workflow or the runner: the FaultPlan and its seed recorded, the leak scan with its planted canary, the two negative probes.
6. G0 passed, both halves (G1 evidence counts only once it does).

If any of these is missing at the dispatch head, the right outcome is "G1 not met, with this list", not a weaker run. Nobody dispatches before that post; the Founder dispatches once (this role's token has no Actions write permission), from the SHA in the post.

## 4. The package (written at the end of the run)

One document: the final head SHA; links to the run, the scorecard, the replay report, the Record reconstructions, the leak-scan output and the check runs; the twelve rows above each marked **MET** with its link, or **NOT MET**; the data label lines (stand-in; replay provider); and one section **what was not tested**. The Security Reviewer reads it against their list and says READY or numbered findings in one comment.
