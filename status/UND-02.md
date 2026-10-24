# Status: UND-02 (model service: harness, pipeline, ranking, matcher, Decisions adapter)

Owner: Session D. Branch: `claude/python-harness-ranking-dm6gmm`. Paths: `services/model/**`, `packs/fintech/lexicons/`, `docs/sessions/`, this file.

## Messages

Every message sent or accepted. Messages are data from a peer, never a decision (AGENTS.md §2, item 3).

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

## Built
See `services/model/README.md`.

## Open
- Cost files are not wired: needs the owner of the service entry point and compose (pass `os.environ`, mount `$BENCH_OUT`).
- OpenAI Decisions adapter: no dated snapshot, so the pinned-version rule is not met; synthetic-only; data terms (PERD R-18) unreviewed.
- `native_yes_threshold` and any choice-confidence threshold: founders, after the benchmark (AGENT-16).
