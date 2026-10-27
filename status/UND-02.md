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

## Built
See `services/model/README.md`. Cost-file wiring (REQ-NFR-007, REQ-OPS-001): `noema_model/main.py` `build_telemetry`; `deploy/compose/bench-model.yml`; `make model-bench-up`.

## Open
- Cost files reach a file only once something constructs the harness from `app.state.telemetry`; `main.py` is still the health-only skeleton. Owner: whoever builds the service's request path.
- `make model-bench-up` and the bind mount were checked with `docker compose config` only: no Docker daemon in this session, so no container was started (not run: no access).
- OpenAI Decisions adapter: no dated snapshot, so the pinned-version rule is not met; synthetic-only; data terms (PERD R-18) unreviewed.
- `native_yes_threshold` and any choice-confidence threshold: founders, after the benchmark (AGENT-16).

## Manual container test (separate PR)

- `.github/workflows/bench-model-up.yml`: `workflow_dispatch` only, `ubuntu-24.04`; prepared and not run; it needs no secret.
- Messages: the founder's decisions of 25 Oct 2026 (the request path and this workflow as separate pull requests, the workflow not run yet, Linux runner only, messages logged) are logged in the first PR's table (row dated 2026-10-25). This PR logs one more: the decision of 27 Oct 2026 below.
- Checked without running the workflow: YAML parses; `check_pins`, `check_env_refs` and `check_secret_refs` pass; the Python snippet and the host-side check were run against a temp directory (one file, JSON equals the marker, owner is the invoking user); `docker compose config` with the override resolves. Not run: the stack itself (no Docker daemon in the session).
- 2026-10-27, founder (relayed by the user), to D: decided that instruction text is removed from the PR text and status files of #65 and #66, a log of decisions staying; after #71 and #73 are merged, `origin/main` is merged into each of #65 and #66 and the checks run once on each final head. Used for: this change.
