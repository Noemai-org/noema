# Status · STM-01-wire-trust · `buildTrust` (composition follow-up)

Updated 2026-11-18. Owner: Engineering Lead. REQ-TRU-009, REQ-RCV-003; INV-03, INV-12, INV-25, INV-29. Needs: noema#710 (merged; the Trust adapter), noema#708 (open; `keyfiles.Load`).

## State

- **Red stage.** Tests first, with a stub that builds nothing: `services/core/orchestrator/portadapters/trustpg/*_test.go` (PostgreSQL, CI only: pgsodium) and `services/core/cmd/core/wire_trust_test.go`.

## Open

1. The intake service (`trust/intake.Service`) is not built here: 11 of its 15 ports (Writes, Capabilities, Scope, Authority, Candidates, Ownership, Evidence, Reconciler, Policies, Clocks, Confirmations, Planner, Recorder) have no adapter on `main`. `buildTrust` takes the `Evaluator` as an argument and fails closed when it is nil.
2. `keyfiles.Load` is on noema#708, not on `main`; `buildTrust` takes a `KeyLoader` so this PR compiles without it. main.go passes `keyfiles.Load` once #708 merges.
3. Out of scope, follow-up after #710: the CorrelationID signature change of `Trust.Decide`.

## Session 2026-11-18 · STM-01-wire-trust

- Tests added (REQ-TRU-009, REQ-RCV-003, REQ-SEC-004; INV-03, INV-12, INV-29): see above.
