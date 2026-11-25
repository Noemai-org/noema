# platform/trustlimits

**What it does.** Reads `trust-limits.v2` (the pack's `limits/trust_limits.json`): the two provisional limits the Trust steps use as data, not code. `trustlimits.Load(r io.Reader, packID string) (Limits, error)` has no default, and the caller names the pack it loads for (a document of another pack is refused). A document that is not exactly the contract (a missing, unknown or repeated member, a value outside the bounds of ruling A233 (interim): the ambiguity floor at least 7000 and at most 10000, the AT_RISK share 1000 to 5000, so **0 is refused: it would switch the check off**; the bounds are constants in `platform/ceilings`, trailing content) is a `*trustlimits.Error` whose text carries the reason and none of the document (the decode cause is behind `errors.Unwrap`). The zero `Limits` reports `Loaded() == false` and **its accessors return `ErrNotLoaded` and 0, never a value**, so a Trust step that forgets `Loaded()` cannot read a zero as a limit. `Label()` is the document's label (SYNTHETIC or VALIDATED): the values are provisional while it is SYNTHETIC.

**Values (integer basis points, A166).** `AmbiguityAbsoluteFloorBP() (int, error)`, the auto-resolve absolute floor (PERD v1.23 line 1772, REQ-AMB-003; the pack says 7000); `ClockAtRiskRemainingBP() (int, error)`, the share of a clock's duration under which the phase is AT_RISK (line 2134, REQ-TRU-006; the pack says 2500). Both are NON-CANONICAL until GOV-06 adopts the PERD 67.3 defaults. The auto-resolve margin and the candidate cap are not here: they are in the pack's intent nodes.

**Bounds (A233, interim, relayed):** a pack may raise the floor above 7000 and never lower it; the AT_RISK share is 1000 to 5000 (the PERD value 2500 is inside). Both values stay provisional and NON-CANONICAL until GOV-06 adopts them.

**Status.** The loader exists; no Trust step reads it yet (the steps arrive with the Trust intake). Ruling A205 Q1; AGENT-16.

**Tests.** `go test -count=1 ./platform/trustlimits/` from `services/core/`.

**Start-up (A612).** `LoadPackFile(files, path, wantPackID)` reads the file the composition names from the verified `Pack.Limits` map through `Load`; a malformed, v1 or incomplete file, or a path that is not pinned, is a `*Error` and the core must not start. The package names no file (INV-14); `cmd/core` does not call it yet because the path needs EL's composition (no new setting, A330).
