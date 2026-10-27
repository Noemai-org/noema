# record/chain

**What it does.** Appends events to the per-tenant hash chain inside the caller's transaction, and verifies a chain from genesis.

**REQs.** REQ-AUD-001 (append-only, hash-chained; an append failure fails the enclosing transaction, EC-E16). `Verify` is what REC-05 and REC-06 will run nightly and in CI.

**How it works.**
- `Appender.Append(ctx, tx, Event)` calls `record_lock_head` (locks the tenant's chain-head row, so appends are serialised per tenant), computes `hash = SHA-256(prev_hash || JCS(event without hash))` (PERD §32.1) and inserts through `record_insert`, which refuses an event that does not extend the head it locked. No power holds any privilege on the Record tables (`services/core/migrations/README.md`).
- The first event of a tenant's chain has `prev_hash = SHA-256("noema:genesis:" || tenant_id)` (PERD §32.1), written in the contract's `sha256:<hex>` form.
- The event carries the SoT §43.6 fields, the PERD §32.1 additions (`seq`, `payload_hash`, `actor`) and, on `SECURITY_EVENT` and `GRANT_VOIDED`, `kind` and `reason`. The object the event refers to or details is `Payload`: its canonical hash is `payload_hash`, and the object itself is stored in `record_event.payload`, outside the hash, so `Verify` can check one against the other.
- IDs, tenant, correlation, object reference and writer names are checked against the contract patterns before they reach the database.

**Contract.** The events are `contracts/schemas/record-event.v2.json` (DEC-2026-10-27-record-event-v2): `schema_version` "2.0", `seq`, `payload_hash`, `actor`, the tenant-scoped genesis hash. `record-event.v1.json` is not edited and is not written. The closed sets (event types of PERD §32.2, security kinds, nine grant-void reasons, provider-failover reasons) are in `types.go`; a test requires them to equal the schema's enums, and the writer rejects an unknown member before it reaches the database. `Verify` also rejects an event whose `schema_version` is not "2.0".

**Tests.** `go test ./record/chain/...` from `services/core/`: the golden vectors of `contracts/fixtures/golden_vectors.json` (written by `tools/record_chain_ref.py`, a Python reference written from PERD §32.1) replayed with and without the database, contiguity and tenant isolation, concurrent writers, a failed append rolling the action back, the stale-head refusal, eight kinds of tampering each detected with its break kind, the appending role's lack of UPDATE/DELETE, and refusal of events outside the contract. They need PostgreSQL server binaries or `NOEMA_TEST_DATABASE_URL`.
