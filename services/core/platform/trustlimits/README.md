# platform/trustlimits

**What it does.** Reads `trust-limits.v1` (the pack's `limits/trust_limits.json`): the two provisional limits the Trust steps use as data, not code. `trustlimits.Load(r io.Reader, packID string) (Limits, error)` has no default, and the caller names the pack it loads for (a document of another pack is refused). A document that is not exactly the contract (a missing, unknown or repeated member, a value that is not an integer from 1 to 10000, **0 included: it would switch the check off**, trailing content) is a `*trustlimits.Error` whose text carries the reason and none of the document (the decode cause is behind `errors.Unwrap`). The zero `Limits` reports `Loaded() == false` and **its accessors return `ErrNotLoaded` and 0, never a value**, so a Trust step that forgets `Loaded()` cannot read a zero as a limit. `Label()` is the document's label (SYNTHETIC or VALIDATED): the values are provisional while it is SYNTHETIC.

**Values (integer basis points, A166).** `AmbiguityAbsoluteFloorBP() (int, error)`, the auto-resolve absolute floor (PERD v1.23 line 1772, REQ-AMB-003; the pack says 7000); `ClockAtRiskRemainingBP() (int, error)`, the share of a clock's duration under which the phase is AT_RISK (line 2134, REQ-TRU-006; the pack says 2500). Both are NON-CANONICAL until GOV-06 adopts the PERD 67.3 defaults. The auto-resolve margin and the candidate cap are not here: they are in the pack's intent nodes.

**Open (Architect):** 1 is the least structural value (0 is "off"), but a floor of 1 basis point is nearly off too. A least safe value for each limit, kept in `platform/ceilings` with its ruling reference, is the Architect's to rule; until then a pack may set any value from 1 to 10000.

**Status.** The loader exists; no Trust step reads it yet (the steps arrive with the Trust intake). Ruling A205 Q1; AGENT-16.

**Tests.** `go test -count=1 ./platform/trustlimits/` from `services/core/`.
