# platform/trustlimits

**What it does.** Reads `trust-limits.v1` (the pack's `limits/trust_limits.json`): the two provisional limits the Trust steps use as data, not code. `trustlimits.Load(io.Reader) (Limits, error)` has no default. A document that is not exactly the contract (a missing, unknown or repeated member, a value that is not an integer from 0 to 10000, trailing content) is a `*trustlimits.Error`, and the zero `Limits` reports `Loaded() == false`.

**Values (integer basis points, A166).** `AmbiguityAbsoluteFloorBP()`, the auto-resolve absolute floor (PERD v1.23 line 1772, REQ-AMB-003; the pack says 7000); `ClockAtRiskRemainingBP()`, the share of a clock's duration under which the phase is AT_RISK (line 2134, REQ-TRU-006; the pack says 2500). Both are NON-CANONICAL until GOV-06 adopts the PERD 67.3 defaults. The auto-resolve margin and the candidate cap are not here: they are in the pack's intent nodes.

**Status.** The loader exists; no Trust step reads it yet (the steps arrive with the Trust intake). Ruling A205 Q1; AGENT-16.

**Tests.** `go test -count=1 ./platform/trustlimits/` from `services/core/`.
