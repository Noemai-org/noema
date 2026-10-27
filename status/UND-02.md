# Status: UND-02 (model service: harness, pipeline, ranking, matcher, Decisions adapter)

Owner: Session D. Branch: `claude/python-harness-ranking-dm6gmm`. Paths: `services/model/**`, `packs/fintech/lexicons/`, `docs/sessions/`, this file.

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
