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
