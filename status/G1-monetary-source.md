# Status · monetary_calls source (Benchmark Owner agent)

Updated 2026-11-05. Decision: the Founder, 4 Nov 2026, source (a): a per-route request log in the hostility proxy, built by the Integration Lead (ENV-09, noema#159, merged).

## State
- **Built (this change):** `benchmark/oracle_client/proxy_log.py` (`ProxyRouteLog`: mark, since, describe), its plug-in to the oracle client (`Baseline.monetary_mark`), the runner flag `--proxy-url`, and the read-only classification of the proxy's route table with a drift guard. Tests first (seen failing).
- **What it counts:** requests FORWARDED on a route not classified read-only, since the baseline. Refused requests are not counted; a write whose answer was dropped counts; a log that dropped entries, shrank or cannot be read is unavailable, never zero.
- **Today** every route in the table is read-only, so the count can only be above zero if a route is added to the table (the guard forces its classification) or an unclassified one is forwarded.

## Open
1. The counter sees what passes the proxy. A call that bypasses it would not be seen; the compose topology (SEC-11, merged) is what keeps the runtime's egress on the proxy.
2. For the live-Tushe rerun, Tushe's access log (read through `tushe-admin`, per-run approval) is a second source; not built.

## Messages
### 2026-11-04 (time not recorded) accepted · Founder (decision, relayed by the user) → Benchmark Owner agent
- Purpose: monetary_calls source (a), a per-route request log in the hostility proxy, built by the Integration Lead.
- Receiver used it for: this change (the reader).
- Evidence: this pull request.

## Session 2026-11-05 · BEN-05 monetary source, generation requirement
- Codex (noema#170, second round): an absent `generation` let a restart that recreates the same prefix pass. The reader now fails closed: a proxy log that reports no `generation` is `CounterUnavailable`, never zero.
- The proxy (ENV-09) does not report `generation` today (`control.go` returns entries, counts, dropped, log_hash). Until the Integration Lead adds a monotonic per-start `generation` to `GET /control/v1/requests`, `monetary_calls` is unavailable and the G1 run reports NOT RUN for it. Request to the Integration Lead: add `generation` (changes on every proxy start and every log reset).

## Session 2026-11-06 · BEN-05 monetary source, real endpoint fields
- Codex (noema#170, third round): ENV-09 `getRequests` returns no `generation`, so requiring it made the counter unreachable against the real proxy. Reverted to option (1): continuity is checked from fields the endpoint returns (marked entry `seq`, a digest of the marked prefix), and a `generation` is enforced strictly whenever the proxy reports one (a generation that appears, changes or disappears is unavailable).
- Residual, stated in the counter's `describe()` and so in the run manifest: a restart that replays a byte-identical prefix is not detectable without a generation. Closing it needs a versioned `generation` on ENV-09 (Integration Lead; not requested as a blocker).

## Session 2026-11-06 · BEN-05 monetary source, ruling A22
- Ruling A22 (6 Nov 2026): the Integration Lead adds a per-start `generation` to `GET /control/v1/requests`, with a test. Codex (noema#170, fourth round, P1): without it a restart that replays a byte-identical prefix is not detectable, so the count could be silently low.
- The reader is back to failing closed, now on purpose: a log that reports no `generation` (or an empty or non-string one) is unavailable, never zero, at the baseline and again at the readout. `monetary_calls` is therefore NOT RUN against the proxy on `main` today and becomes available the moment the Integration Lead's change merges. Tests: no generation, a malformed generation, a generation that changes or disappears after the mark, an identical-prefix restart with a new generation, and a reset that refills with different entries under the same generation.
- Merge order: do not merge before the Integration Lead's generation change (A22); until then this PR only makes the counter honest about being unavailable. The stub in the tests reports a generation by default and says so; the one test of the real endpoint's current shape asserts "unavailable".
- Pre-work for the next agent: when the proxy reports `generation`, run the G1 monetary readout against it once and cite the run.
