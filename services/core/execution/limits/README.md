# execution/limits

Loads the circuit-breaker policy (`MinSamples`, `Cooldown`) and the dispatch budget from pack data, as `execution-limits.v1` (Architect ruling A3; REQ-CAP-003, REQ-RCV-003, INV-07, AGENT-16). The numbers are in `packs/fintech/limits/execution_limits.json`, not in code. A document that is not exactly the contract is refused; there are no defaults.

- `Load(r)` returns `Limits{Breaker breaker.Policy, DispatchBudget int}`, or a `*limits.Error` with the code `SCHEMA_INVALID` (the decode cause is kept for `errors.Unwrap`).
- The budget key is `tenant_id`, `correlation_id`, `capability`, `target_ref`, in that order. JSON Schema checks the four names; `Load` checks the order.
- `Cooldown` runs on the injected clock (INV-21), never wall time.
- The breaker supports one half-open probe, so `half_open_probes` must be 1.

Contract note (AGENT-15): `execution-limits.v1.json` is a new file. No existing schema changes meaning, and no pack changes. Both Founders approve the new contract file.

Tests: `go test ./execution/limits/` from `services/core`.
