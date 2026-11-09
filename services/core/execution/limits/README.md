# execution/limits

Loads the circuit-breaker policy (`MinSamples`, `Cooldown`) and the dispatch budget from pack data, as `execution-limits.v1` (Architect ruling A3; REQ-CAP-003, REQ-RCV-003, INV-07, AGENT-16). The numbers are in `packs/fintech/limits/execution_limits.json`, not in code. A document that is not exactly the contract is refused; there are no defaults.

- `Load(r)` returns `Limits{Breaker breaker.Policy, DispatchBudget int}`, or a `*limits.Error` with the code `SCHEMA_INVALID` (the decode cause is kept for `errors.Unwrap`).
- The budget key is `tenant_id`, `correlation_id`, `capability`, `target_ref`, in that order. JSON Schema checks the four names; `Load` checks the order.
- `Cooldown` runs on the injected clock (INV-21), never wall time.
- The breaker supports one half-open probe, so `half_open_probes` must be 1.

Contract note (AGENT-15): `execution-limits.v1.json` is a new file. No existing schema changes meaning, and no pack changes. Both Founders approve the new contract file.

Tests: `go test ./execution/limits/` from `services/core`.

**Ceilings (SR's note 1 on noema#195).** `Load` accepts `min_samples` and `max_dispatches` up to 2147483647. A pack with `max_dispatches` 2147483647 passes the loader and defeats the dispatch budget, and a huge `min_samples` means the breaker never opens. The numbers are Founder-reviewed pack data, not code, and choosing a lower ceiling is a policy decision (REQ-AGT-005), so the loader does not invent one. Pack review (PCK) must reject a budget or a sample count above the value the Founders set; until they set one this README is where the gap is written.

**Ceilings for the POC pack (the Founders' instruction of 8 Nov 2026, to be committed as a decision record; cite it as authority only once it is).** `max_dispatches` is at most 2 (A3) and `min_samples` is at most 20 (A3). Any higher value in a pack needs a decision record. The loader still accepts values up to 2147483647 and does not enforce these ceilings (it invents no number, REQ-AGT-005); pack review (PCK) rejects a pack above them. A test in the pack's own suite is the place to enforce them; none exists yet.

