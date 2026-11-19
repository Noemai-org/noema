# orchestrator/portadapters/trust/reconciler

The Trust intake's Reconciler (T10), package `reconcilerport` (TRU-07; ruling A329). Updated 18 Nov 2026. All data is SYNTHETIC.

**Implements:** REQ-RES-001, REQ-RES-002, REQ-TRU-003, REQ-TRU-007, REQ-CTX-004. **Never:** a write, a login that is not the tenant's, a fallback to another tenant's login (INV-11, INV-12), a model call, a reason code or a capability ID of its own (INV-14), a default for a setting (AGENT-16), a payload, a row, an ID, a tenant or a store's error text in an error (AGENT-13).

`Adapter.Reconcile(ctx, tenantID, snapshotID)` implements `intake.Reconciler`. In order:

| Step | Rule | Refusal |
|---|---|---|
| Wiring | snapshots, classifier factory, pack and a positive timeout are set | `ErrConfig` |
| Context | the caller's context has a deadline; the call is also bounded by `Config.Timeout` | `ErrNoDeadline`, then `ErrRead` over the deadline |
| Tenant, ID | a tenant is named; the snapshot ID has the shape `evs_` and 2 to 32 letters or digits | `ErrSession`; `ErrNotFound` |
| Read | `Snapshots.Sealed` as the tenant's own login. A missing snapshot and another tenant's give one answer | `ErrNotFound` (same text), `ErrRead` for a store that cannot answer |
| Row | the snapshot returned is for the tenant and ID asked | `ErrNotFound` |
| Intact | at least one item; the items are exactly the sealed IDs; the evidence set hash recomputed from their payload hashes is the stored one; a seal time exists | `ErrIntegrity` |
| Sealed value | the sealed classification is one of the twelve of PERD 22.3 | `ErrBadAnswer` |
| Again | the items are classified again by `evidence/reconciler` (real rule set and mapping, payloads through `payloadread` as the tenant) at the snapshot's time; the answer must be the sealed classification | `ErrReclassify`, `ErrDrift` |
| Answer | the sealed classification; `Frozen` when it is `CONTRADICTORY_STATE`; the pack's escalation reason (must be `<vertical>.reason.<name>`, or empty); a copy of the pack's allow-list | `ErrBadAnswer` |

**Drift is a signal.** If the classification made again is not the sealed one the call fails closed with `ErrDrift`. It returns neither value and never overwrites the snapshot (INV-21). T10 turns any error into UNKNOWN `INTERNAL_ERROR`.

**Time.** A rule can read `now()`, so the classification is made again at the later of the snapshot's `sealed_at` and the latest item `read_at` (both from the snapshot), never at the time of the question. `ReconcilerAt(base)` is the classifier factory over the real Reconciler: the base configuration with a clock that reads that time.

**Not compared: the reconciliation ID.** It hashes the clock reading, which the snapshot does not record, so it cannot be reproduced. T10 does not take it. Open: record the clock reading in the snapshot (a contract change, not here).

**The freeze.** No `contradiction_freeze` table exists yet (RES-02), so `Frozen` is derived from the sealed classification alone. A freeze lifted by a Trust resolution cannot be seen here, and the target stays frozen.

**Store.** `NewStore(PoolsOf(pools))` over cmd/core's per-tenant Trust pools (`NOEMA_TRUST_DSN_DIR`). One read-only repeatable-read transaction of the tenant's login reads the snapshot (`context_snapshot`) and its items (`evidence_item`); both queries bind `tenant_id`, and the database's row-level security binds it again (migrations 0008 and 0020; `trust_rw` holds SELECT on both). The transaction is always rolled back. The item's freshness limit is the row's `freshness_sla_seconds`: the stricter limit applied at read time is not stored, and it feeds only an expiry, never a classification. Errors: `ErrNoLogin`, `ErrNotFound`, `ErrStore` (cause reachable with `errors.Is`, never printed).

**Setting (A330, no code default).** `NOEMA_TRUST_RECONCILE_TIMEOUT_MS`: the composition passes it as `Config.Timeout`. The package reads no environment.

**Pack data.** `Pack` gives the escalation reason of a classification and the capabilities allowed under a freeze. The pack does not carry them in a loadable form yet, so the composition (`wire_trust.go`) supplies them.

**Tests.** From `services/core/`: `go test -race -skip TestDB ./orchestrator/portadapters/trust/reconciler/` needs no database. `TestDB*` need `NOEMA_TEST_DATABASE_URL` (CI). Written first; the mutants that were killed are in `status/TRU-07-reconciler.md`.
