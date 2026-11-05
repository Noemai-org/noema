# execution/breaker

Per-capability limits from the descriptor. Built by CAP-01 (slice 1, 2026-11-05).

**Implements:** REQ-CAP-003 (limits, timeouts and circuit breakers per capability). SoT IDs: ASI08, SoT §43.8.

## What it does

- `ParamsOf(descriptor.limits)` reads `timeout_ms`, `rate_per_principal_per_hour` and `circuit_breaker.{error_rate, window_s}`. `error_rate` must be a plain decimal in (0, 1] (kept exact, no float); anything else is refused.
- `New(clock, params, policy, onChange)` builds one circuit. It opens when, in the last `window_s` seconds, there are at least `MinSamples` outcomes and the failure ratio is at or above `error_rate`. While it is open, `Allow` refuses every call with an `*OpenError` (the caller returns UNKNOWN or ESCALATE, never a guess; dispatches are 0). After `Cooldown` one probe is allowed (`HALF_OPEN`); a good probe closes the circuit with an empty window, a bad one reopens it. A `Permit` settles once, and a permit from before a state change is ignored. `onChange(from, to)` is where the caller records `CIRCUIT_STATE_CHANGED`; notifications are queued under the lock and delivered one at a time, in transition order, outside it (`Flush` waits for the queue to drain).
- `NewRateLimiter(clock, params).Allow(principal)` allows at most `rate_per_principal_per_hour` calls per principal in any sliding hour; an empty principal is refused.
- `Breaker.Timeout()` is the descriptor's timeout; the caller applies it as a context deadline.

## Not decided here

`MinSamples` and `Cooldown` are not in the descriptor, so they are required inputs with no default (a threshold is a policy decision, AGENT-16). The descriptor's illustrative values are 0.5 over 60 s; the pack's own are data.

## Not built yet

Per-tenant keying of breakers (one `Breaker` per tenant and capability is the intended use), recording `CIRCUIT_STATE_CHANGED`, wiring into the Execution Controller (an open circuit gives UNAVAILABLE evidence), and EC-E08 end to end.

## Tests

`go test -race ./execution/breaker/` (no database; simulated clock). A `rapid` property: whatever the calls, results and clock steps, no call is allowed while the circuit is open and its cooldown has not passed.
