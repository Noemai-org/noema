# Status · CAP-01 (Core Engineer 1) · per-capability limits, timeouts and circuit breakers

Updated 2026-11-05. Plan task CAP-01: owner C1+AG; REQ-CAP-003; Needs EXE-01 (merged); done when "An open breaker yields UNAVAILABLE evidence, never a guess".

## State: slice 1 in review (draft until CI is green at the final head and Codex is READY). CAP-01 is not Done.

## Session 2026-11-05 · CAP-01
### Session summary
- What changed (PR links): the CAP-01 pull request on `feat/CAP-01-breaker`: package `execution/breaker` (`ParamsOf`, `Breaker`, `Permit`, `RateLimiter`), README, tests.
- Why: REQ-CAP-003; contain cascading failure (ASI08); an open circuit means no dispatch.
- Risks, and how they are contained: a circuit that opens wrongly stops a capability; one that never opens lets a failing target be hammered. The numbers the descriptor does not carry (`MinSamples`, `Cooldown`) are required inputs with no default, so no threshold is invented here. The error rate is kept exact (a decimal string, `big.Rat`): no float in `execution/`.
- Tests added (REQ-CAP-003): 11 tests, one `rapid` property (plus, after Codex: durations that would overflow are refused, the limiter forgets quiet principals, state-change notifications are delivered in order and one at a time, each caller waits for its own, a failed record of a permissive change reopens the circuit): descriptor parsing and 10 kinds of bad value; the policy is required; opens exactly at the rate and not below, and not before the minimum samples; an open circuit allows nothing until the cooldown; one probe, then closed or reopened with the right transitions; outcomes outside the window do not count; a permit settles once; concurrent use under `-race`; the hourly per-principal rate. Seen failing first (the package did not exist).
- Mutations run and reverted (all caught): opens only above the rate; minimum samples ignored; cooldown ignored; window never prunes; a permit settles more than once; rate window never expires; rate off by one; no probe flag after the cooldown; a good probe reopens. (Two of my first mutation runs did not compile or did not match and counted as no result; redone.)
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: slice 2, wire a `Breaker` per tenant and capability into the Execution Controller before E8 (an open circuit gives UNAVAILABLE evidence and an ESCALATE input to Trust), record `CIRCUIT_STATE_CHANGED` in `onChange`, and EC-E08.
- Open questions (owner): the values of `MinSamples` and `Cooldown` (Architect; REC proposal: 4 and the descriptor's window, but that is a policy number); where the policy lives (`platform/config` is empty).
- Files to read first: `services/core/execution/breaker/README.md`.
- Do not touch: noema#94, noema#90 (held, DEBT-01).

- Known gap, stated: the `b.gen == tk.gen` guard in the fail-closed undo is defence in depth that no test can reach, because nothing can change the state while a HALF_OPEN or CLOSED notification is pending (the mutation that removes it survives).
