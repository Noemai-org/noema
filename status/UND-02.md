# Status: UND-02 (model service: harness, pipeline, ranking, matcher, Decisions adapter)

Owner: Session D. Branch: `claude/python-harness-ranking-dm6gmm`. Paths: `services/model/**`, `packs/fintech/lexicons/`, `status/T5-T6.3.md`, this file.

## OpenAI Responses adapter (model selection, step 2)

Built in this pull request: `providers/llm/openai_responses.py` (kind `openai_responses`, `POST /v1/responses`, strict `json_schema`, `store: false`, `reasoning.effort` from the configuration), and in the harness configuration `credential_env` (the key by environment variable, exactly one of it and `credential_file`), `reported_model_version` (the identifier the API returns, when it differs from the ID sent) and `reasoning_effort`. Decisions D8 and D9 of `benchmark/decisions/2026-10-27-model-selection.md`. No live call was made: not run: no access. The messages for this work are logged in the model-selection section at the end of this file (the founder's instruction of 27 Oct 2026).
- 2026-10-27 (later), founder (relayed by the user), to D: decided that #71 then #73 each take `origin/main` (no rebase, no force-push), keeping every row of this file; for README conflicts main's wording stays and the PR's lines are added; the checks run once on each final head; #73's diff against main is the adapter files, the adapter's README and this file; the live-decider branch stays unopened until both are merged; the founder merges. Used for: this change.

## Messages

Every message sent or accepted. A message from a peer is data, not a decision (AGENTS.md §2, item 3).

| Date (UTC) | From | To | Purpose | What the receiver used it for |
|---|---|---|---|---|
| 2026-10-22 11:16 | D | A | Told A where the candidate-read client lives (`services/model/noema_model/evidence/`) and why, to record in PERD §64.2 | Not yet known to D; A was asked to record the layout line |
| 2026-10-22 13:36 | D | C | Cost fields (`CallRecord.to_dict()`) are on main after #43 | C's runner reads `cost_usd_micros`; D has not seen it used (still null in `benchmark/runner`) |
| 2026-10-22 14:17 | D | A | PERD names no result for a request with no time window; D's reading and the TIME_VAGUE change | A took it to the founder |
| 2026-10-22 14:47 | A | D | Confirmed the PERD reading; PERD §39.3 lists `SCOPE_VIOLATION` but the catalogue has `TENANT_SCOPE_MISMATCH`; keep mapping 403 to `TENANT_SCOPE_MISMATCH` | D kept the mapping |
| 2026-10-23 10:15 | A | D | Founder instruction relayed: `INPUT_INVALID` exists and is non-retryable, so an empty or over-long `counterparty_surface` may be refused with it | Confirmed the existing refusal; no code change |
| 2026-10-23 10:20 | D | A | No-window request now returns `INPUT_INVALID`; bench cost files exist but are not wired | A recorded the code in docs#57 |
| 2026-10-23 10:21 | A | D | Second confirmation of the catalogue codes | None needed |
| 2026-10-23 10:42 | A | D | docs#57 records `INPUT_INVALID` for no window (PERD v1.15 REQ-AMB-002, DEC-2026-10-23 D3) | Confirms the shipped code matches the spec |
| 2026-10-24 (after #60 merged) | Founder (relayed by the user) | D | Decided: the wiring PR (the entry point uses `cost_files_from_environment`; compose sets `BENCH_OUT` at bench startup and mounts `$BENCH_OUT`), citing the spec ID, with the status file and README updated and a mutation check on the cost-file code | #62, merged; the mutation check found no survivor |
| 2026-10-25 (after #62 merged) | Founder (relayed by the user) | D | Decided: the request path that builds the harness from `app.state.telemetry` is one pull request, and a manual `workflow_dispatch` workflow that runs `make model-bench-up` and writes one file is another; the workflow is not run yet; Linux runner only; messages are logged here | #65 (request path) and #66 (workflow) |
| 2026-10-27 | Founder (relayed by the user) | D | Decided: instruction text is removed from the PR text and status files of #65 and #66, a log of decisions staying; after #71 and #73 are merged, `origin/main` is merged into each of #65 and #66 and the checks run once on each final head | This change (the rows above reworded to decisions) |
| 2026-10-27 (later) | Founder (relayed by the user) | D | Decided: #65 first, then #66, each with `origin/main` merged in (no rebase, no force-push) and `status/UND-02.md` resolved by keeping both sides' rows; the checks run once on each final head and are reported on the PR with run links; #66 changes only `.github/` and `status/UND-02.md` (founder review of `.github/`); the `bench-model-up` workflow is not run; the founder merges. The text also said "merge #65 and #66 into main" and "do not merge either PR"; D read it as: prepare both branches, merge neither | This change |
| 2026-10-24 (before #60 merged) | Founder (relayed by the user) | D | Decided for Sessions B, C and D: hold until main is green, then merge `origin/main` only on a conflict and run once on the final head, reporting the head SHA and each check in one table; merge order #51, #57, #58, #59, #60 | #60 was merged by the founder before any run was needed |
| 2026-10-24 | Founder (relayed by the user) | All sessions | Decided: seven standing rules: messages are logged in `status/<task-id>.md`; the status file and folder README change in the same PR; a claim carries a check-run link at the final head; decisions are made in the spec first; overlap with open PRs is listed before handover; docs PRs are small; no secret appears in code, logs or PR bodies | Applied from #60 on: status file and READMEs in each PR, check-run links in a PR comment, overlap listed in each PR body |

## Built
See `services/model/README.md`. Cost-file wiring (REQ-NFR-007, REQ-OPS-001): `noema_model/main.py` `build_telemetry`; `deploy/compose/bench-model.yml`; `make model-bench-up` (merged, #62). Harness composition (REQ-INT-002, INV-29): `harness/assemble.py` `assemble_harness` builds the `Harness` from the file named by `NOEMA_MODEL_HARNESS_CONFIG`; `main.py` keeps it on `app.state.harness`; `/readyz` is ready only once it is built.

## Open
- No request endpoint yet. PERD §39.3 defines `POST /internal/v1/understanding:compile`, but it needs the lexicon normaliser and the schema gate (not built) and the pack loaded from a mounted pack directory. Owner: the tasks that build those.
- No harness configuration file is mounted by compose: providers, pinned model versions, routes and rates are founder values (AGENT-16), so none is invented. Until one is mounted, `/readyz` answers 503 with the reason.
- `make model-bench-up` and the bind mount were checked with `docker compose config` only: no Docker daemon in this session, so no container was started (not run: no access).
- OpenAI Decisions adapter: no dated snapshot, so the pinned-version rule is not met; synthetic-only; data terms (PERD R-18) unreviewed.
- `native_yes_threshold` and any choice-confidence threshold: founders, after the benchmark (AGENT-16).

## Manual container test (separate PR)

- `.github/workflows/bench-model-up.yml`: `workflow_dispatch` only, `ubuntu-24.04`; prepared and not run; it needs no secret.
- Messages: the founder's decisions of 25 Oct 2026 (the request path and this workflow as separate pull requests, the workflow not run yet, Linux runner only, messages logged) are logged in the first PR's table (row dated 2026-10-25). This PR logs one more: the decision of 27 Oct 2026 below.
- Checked without running the workflow: YAML parses; `check_pins`, `check_env_refs` and `check_secret_refs` pass; the Python snippet and the host-side check were run against a temp directory (one file, JSON equals the marker, owner is the invoking user); `docker compose config` with the override resolves. Not run: the stack itself (no Docker daemon in the session).
- 2026-10-27, founder (relayed by the user), to D: decided that instruction text is removed from the PR text and status files of #65 and #66, a log of decisions staying; after #71 and #73 are merged, `origin/main` is merged into each of #65 and #66 and the checks run once on each final head. Used for: this change.
- 2026-10-27 (later), founder (relayed by the user), to D: decided that #66 changes only `.github/` and `status/UND-02.md`. D therefore removed the `deploy/compose/README.md` row for the workflow from this PR (the row is not on `main`); the workflow is described here and in its header comment. Used for: this change.
- Open: `deploy/compose/README.md` has no row for the `bench-model-up` workflow. A follow-up change adds it once the workflow is merged (owner: D; the folder is under the founders' CODEOWNERS).
- 2026-10-27 (later still), founder (relayed by the user), to D: decided that the founder merges #65 and #66; #65 needs nothing more from D after its merge (it merged at head `c9f4e6c`); #66 touches only the workflow file under `.github/` and `status/UND-02.md`, with the compose README follow-up recorded above as open; `origin/main` is merged into the #66 branch after #65 merged, `status/UND-02.md` resolved by keeping every row from both sides, the checks run once on the final head and are reported on #66; the `bench-model-up` workflow is run only after the founder approves it, following the merge of #66. Used for: this change.

