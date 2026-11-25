# Status · TRU-02 · the production WriteRule over the pack's write_when

Updated 2026-11-24. State: RED pushed (8cdc9de0), GREEN held locally. No PR yet.

# Session 2026-11-24 WAT · TRU-02 (write rule)

## Session summary
- What changed (PR links): none yet. `trustport.PackWriteRule` (new file `writerule.go`): the production `WriteRule` over the node's `write_when` (A543, A364, interim, relayed, not yet committed), evaluated with `internal/predicate` over the intent's slot values. One read-only accessor `trustpack.Pack.WriteWhens` (A551).
- Why: the Writes port takes the slot values since noema#913; cmd/core cannot build `NewWrites` until a `WriteRule` exists (named as a gap by noema#904).
- Risks, and how they are contained: a predicate that cannot be evaluated is false with `ErrWriteUnevaluable`, never true; absent write_when is false (fail closed); the rule is built whole or not at all (a predicate that does not compile over the node's slots, is not Boolean or names the clock refuses the rule).
- Tests added (REQ-TRU-002, REQ-NFR-002; INV-01, INV-14, AGENT-09, AGENT-13): `writerule_test.go`, `trustpack/writewhen_test.go`.
- Regression cases added: none (no bug).
- Debt entries: none.
- Conflicts raised: none. Safer readings are listed in the PR body.

## Pre-work for the next agent
- Exact next step: the composition PR builds `trustport.NewPackWriteRule(pack.WriteWhens())` and passes it to `NewWrites` (not done here: cmd/core is out of this allowlist).
- Open questions (Architect): a required slot with no value is an error here, not null (safer reading); an optional absent slot is null as A364 says.
- Files to read first: `orchestrator/portadapters/trust/writerule.go`, `trustpack/writewhen.go`, `internal/predicate`.
- Do not touch: `writes.go` (its NewWrites comment still says no rule exists; the composition PR updates it).
