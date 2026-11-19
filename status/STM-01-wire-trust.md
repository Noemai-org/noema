# Status · STM-01-wire-trust · `buildTrust` (composition follow-up)

Updated 2026-11-18. Owner: Engineering Lead. REQ-TRU-009, REQ-RCV-003; INV-03, INV-12, INV-25, INV-29. Needs: noema#710 (merged; the Trust adapter), noema#708 (open; `keyfiles.Load`).

## State

- **Built (implementation push):** `trustpg` (`Pools`, `Intents`, `Records`, `KeyDispatches`) and `cmd/core/wire_trust.go` (`buildTrust`, `KeyLoader`, `TrustParts`). Tests first, seen failing at head 2535694 (core run 44331156700: `wire_trust_test.go` and the `trustpg` tests failed on the stub; the boundary test `TestOnlyTheCompositionRootAndAdmissionImportTheReconstruction` also failed, which moved the Record reader behind a local `trustpg.RecordDBs` that `cmd/core` adapts).
- The `trustpg` tests need pgsodium and run in CI only.
- **Design.** Decision and grant records are read as the tenant's `trust_rw` login under forced RLS (tenant also in every WHERE). The record carries the intent ID and hash but no version, so the intent reference `int_<id>@<version>` is found from the `admitted_intent` row with that ID and hash; none means an empty reference and no Binding matches. The dispatch count reads the verified Record chain as the tenant's `record_read` login and counts DISPATCH_ATTEMPTED events by (correlation, capability, target) with the rule of `dispatch_budget_count` (migration 0023; an event whose key cannot be read counts against every key). The `KeyDispatches` doc of noema#710 says GRANT_SPENT events by idempotency key; the database's budget (A153) counts DISPATCH_ATTEMPTED by the four-part key, which is what the intake's `DispatchKey` names, so that is what is read.
- Settings: `NOEMA_TRUST_DSN_DIR` (existing, shared with the payload reader), `NOEMA_TRUST_DB_TIMEOUT_MS` (new; whole milliseconds, A330), `NOEMA_GRANT_KEY_DIR`, `NOEMA_GRANT_KEY_MAX_ACTIVE` (new). No default for any (AGENT-16).

## Open

1. The intake service (`trust/intake.Service`) is not built here: 14 of its 15 ports have no adapter on `main` (only Dispatches has one, noema#710; the intake's own Intents port needs the intent view and target, which the reader built here does not fill). `buildTrust` takes the `Evaluator` as an argument and fails closed when it is nil.
2. `keyfiles.Load` is on noema#708, not on `main`; `buildTrust` takes a `KeyLoader` so this PR compiles without it. main.go passes `keyfiles.Load` once #708 merges.
3. Out of scope, follow-up after #710: the CorrelationID signature change of `Trust.Decide`.

## Session 2026-11-18 · STM-01-wire-trust

- What changed: PR noema#725.
- Why: the composition needs the real Trust and Bindings (STM-01).
- Risks, and how they are contained: reads only; each read as the tenant's own login; a tenant without a login is an error, never another tenant's pool; no key or DSN in any message.
- Tests added (REQ-TRU-009, REQ-RCV-003, REQ-SEC-004; INV-03, INV-12, INV-29): `trustpg/*_test.go` (PostgreSQL), `cmd/core/wire_trust_test.go`.
- Mutation check (hand, `wire_trust.go`): 12 of 13 killed; the survivor (the empty `NOEMA_TRUST_DSN_DIR` check) is equivalent, `loginPools` refuses an empty directory and names the setting. `trustpg` mutants not run: the tests need pgsodium.
- Regression cases added: none (new code). Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: after noema#708 merges, main.go passes `keyfiles.Load` as the `KeyLoader`; the intake `Ports` need the 13 adapters that do not exist yet before `buildTrust` can be given a real Evaluator; then the `CorrelationID` signature change of `Trust.Decide`.
- Open questions (EL): the name and meaning of `NOEMA_TRUST_DB_TIMEOUT_MS` (renamed by A330) and `NOEMA_GRANT_KEY_MAX_ACTIVE`; whether the payload reader and Trust share one pool set (today `loginPools` is called again over the same directory).
- Files to read first: `services/core/cmd/core/wire_trust.go`, `services/core/orchestrator/portadapters/trustpg/`.
- Do not touch: `orchestrator/portadapters/trust/` (noema#710).
