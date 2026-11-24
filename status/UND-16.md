# Status · UND-16 · the compile acceptance run

Owner: C2 · Task: UND-16 (IL, C2): the live compile acceptance run (A198); noema#546, noema#569 (the workflow, merged), noema#654 (this PR).

# Session 2026-11-17 · UND-16 · the runner (WIP exception A253)

## Session summary
- What changed (noema#654): `benchmark/compile_acceptance/` (the golden examples, the recorder, the run loop, the command, the live build from the model service, the leak-scan allow file) and `services/model/tests/test_compile_acceptance_build.py`.
- Why: noema#569 calls `python -m benchmark.compile_acceptance` and `benchmark/compile_acceptance/leakscan.allow`; neither existed, so its dry run failed at the first call. The recordings are UND-15's source (A261).
- Risks, and how they are contained: it spends real money only inside a Founder-dispatched job; the budget guard decides before each request, a stop stays, a failed call is settled at its projection, and a run of no provider call is exit 3, never a pass. The key is read only by the model service's provider adapter. No host and no header is written. The first live reply is not yet seen by this code: stated in the README.
- Tests added (REQ-INT-001, REQ-INT-002, INV-22, INV-29, AGENT-12): 40 benchmark tests and 9 model-service tests; 19 hand mutants over the runner, build, recorder, command and check, all killed (two survived the first tests and each got a case).
- Regression cases added: the stop in the last example, a stop reached inside the transport, a refusal of the last example, a later larger request of an example not replacing the first price, a unit that is not NGN.
- Debt entries: `build.py` repeats the pack view and the minter of noema#635 (`wiring.py`); replace them by an import once noema#635 is on `main` (owner C2, task UND-16).
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: after this merges and the Founder dispatches the dry run, read its cost; then the paid run; then UND-15 reads `recordings/manifest.json` and the request hashes.
- Open questions (owner): the model asked is the frozen `gpt-6-luna` configuration (`--harness-config`, default); a Founder who wants `gpt-6.1-terra` (about ten times the rate) passes the other file.
- Files to read first: `benchmark/compile_acceptance/README.md`, `.github/workflows/bench-live-compile.yml`.
- Do not touch: `benchmark/decisions/harness-*.json` (frozen).

# Session 2026-11-17 · UND-16 · Codex P2 on noema#654 (the dry-run exit code, A285)

## Session summary
- What changed (noema#654): `--dry-run` now exits 3 (NOT RUN), not 0 (`NOT_RUN` constant in `__main__.py`); the module docstring and the README exit table say so; `test_cli.py` expects 3 (and a new case: 3 with and without a key).
- Why: Codex's P2 on `__main__.py:82` and the Architect's A285 ("the code follows the documented exit meanings"). The documented exit 3 is "no key, or no provider call was made: never a pass"; a dry run calls no provider, so a script that gates on the exit code could read an unrun acceptance as green.
- Risks, and how they are contained: `.github/workflows/bench-live-compile.yml` (noema#569) runs the dry run under `set -euo pipefail | tee`, so exit 3 turns its `dry-run` job red and the `run` job (`needs: dry-run`) would never start. That workflow is not in this pull request's allowlist; the change that lets the dry-run job treat exit 3 as "dry run complete" is a separate pull request (SR reads it). Until it merges, do not dispatch the dry run from `main`.
- Tests added (REQ-INT-001; AGENT-11): the dry-run case now asserts 3; one new case asserts 3 for both an empty and a keyed environment. Red first: both failed with `0 != 3` before the change.
- Regression cases added: the two above.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the workflow change (dry run accepts exit 3 and still fails on 1 or 2), then the Founder's word for the dry run.
- Files to read first: `.github/workflows/bench-live-compile.yml`, `tools/ci/test_bench_live_compile.py`.
- Do not touch: `benchmark/decisions/harness-*.json` (frozen).

# Session 2026-11-18 · UND-16 · only a 2xx reply body is recorded (noema#720; SR's condition on noema#654; A320)

## Session summary
- What changed (noema#720): `benchmark/compile_acceptance/recording.py` writes the parsed reply only for a 2xx status; any other status, and a reply that arrives with no status, is recorded with its status and failure label and `reply` and `reported_model_id` null. README: one sentence.
- Why: a provider's error body can echo the request, name the account or carry the tail of a key; replay (UND-15) keys on the request hash, not on an error body. It must merge before the live compile run (A320).
- Risks, and how they are contained: it only removes data from a recording. The cost of a non-2xx call is still settled at its projection by the transport; nothing else reads the body of a non-2xx reply.
- Tests added (REQ-SEC-003; AGENT-13, INV-29): `test_the_reply_body_of_a_response_that_is_not_2xx_is_never_written` (15 non-2xx statuses, each checking the file and the manifest for four leaked strings) and `test_a_2xx_reply_is_still_recorded_and_a_reply_with_no_status_is_not`. Red in CI at `91049f9f`: the `benchmark` job, step `make bench-test`: 1,356 tests, 16 failures, all the new ones. 7 hand mutants of the status condition, all killed.
- One existing test changed its premise, and I say so here: `test_a_body_that_is_not_json_is_kept_as_text_and_never_dropped` recorded a 502 "bad gateway" body and expected it kept, which is the behaviour SR asked to remove. It now uses a 2xx non-JSON body, so it still guards that a 2xx reply that is not JSON is kept as text.
- Regression cases added: the two tests above.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: UND-15 (the recorded-replay Understanding provider) after the Founder dispatches the live compile run: key on `request_sha256`, manifest and labels per A261; a recorded non-2xx call has a status and no reply body.
- Do not touch: the leakscan allow file without a reason.

# Session 2026-11-24 05:00 WAT · UND-16 (Repo Steward) · the stop rule for the live compile runs (A605)

## Session summary
- What changed: this file only (docs, `status/`). It records the stop rule of ruling A605 (Interim, committed in docs#237 as `DEC-2026-11-23-architect-rulings-a602-a605`) in the compile-run record, so every agent that reads a run's findings reads the rule too.
- **The stop rule (A605).** At most **two more live compile runs**: **run 8**, and **run 9 only if run 8's now-readable evidence shows a threshold problem**. If a golden still fails after run 9, **there is no run 10 without a Founder decision**. The Founder chooses between (1) an **asymmetric pass rule**: zero false assertions on status checks are required, and misses of an explicit request are recorded and logged (the safe error); or (2) **delay**. **No golden is edited (A533).**
- What goes with it (A602 to A604, same record): the job log prints, for each golden, the option the model chose, the top five probabilities including abstain, and the threshold applied (option ids and probabilities only; C2). The compile decision splits into a yes/no gate and a capability pick with no abstain option; the gate threshold is **9000 basis points, the Architect's interim value, a Founder decision to confirm** (AGENT-16; open decision OD-P-101 in `docs/open-decisions.md`). BO builds a labelled offline set of 24 to 30 synthetic status-check and "resend" phrasings in en-NG and Pidgin, kept apart from the held-out set (INV-22); C2 fixes the gate against it with the replay provider first. Run 8 repeats the five goldens three times to see variance.
- What this section does not say: it asserts no result of any run after run 6 (`status/UND-16-run6.md` holds run 6's findings). Each later run's findings are written in its own session section as they are read.
- Risks, and how they are contained: docs only; no code, no golden, no contract, no threshold value is changed here.
- Tests added: none. Regression cases: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: read run 8's job log (A602) and recordings; write what they show as a dated section; start run 9 only if the evidence shows a threshold problem; stop after run 9 unless the Founder decides.
- Files to read first: `status/UND-16-run6.md`, `docs/decisions/DEC-2026-11-23-architect-rulings-a602-a605.md`, `docs/open-decisions.md` (OD-P-101).
- Do not touch: the goldens (A533); the held-out set (INV-22).
