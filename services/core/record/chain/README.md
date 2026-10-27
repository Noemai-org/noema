# record/chain

**What it does.** Appends events to the per-tenant hash chain inside the caller's transaction, and verifies a chain from genesis.

**REQs.** REQ-AUD-001 (append-only, hash-chained; an append failure fails the enclosing transaction, EC-E16). `Verify` is what REC-05 and REC-06 will run nightly and in CI.

**How it works.**
- `Appender.Append(ctx, tx, Event)` calls `record_lock_head` (locks the tenant's chain-head row, so appends are serialised per tenant), computes `hash = SHA-256(prev_hash || JCS(event without hash))` (PERD §32.1) and inserts through `record_insert`, which refuses an event that does not extend the head it locked. No power holds any privilege on the Record tables (`services/core/migrations/README.md`).
- The first event of a tenant's chain has `prev_hash = SHA-256("noema:genesis:" || tenant_id)` (PERD §32.1), written in the contract's `sha256:<hex>` form.
- The event carries the SoT §43.6 fields, the PERD §32.1 additions (`seq`, `payload_hash`, `actor`) and, on `SECURITY_EVENT` and `GRANT_VOIDED`, `kind` and `reason`. The object the event refers to or details is `Payload`: its canonical hash is `payload_hash`, and the object itself is stored in `record_event.payload`, outside the hash, so `Verify` can check one against the other.
- IDs, tenant, correlation, object reference and writer names are checked against the contract patterns before they reach the database.

**Known conflict, not resolved here.** `contracts/schemas/record-event.v1.json` says the first event has a `prev_hash` of 64 zeros and has no `seq`, `payload_hash` or `actor` (and `additionalProperties: false`); PERD §32.1 says the genesis `prev_hash` is `SHA-256("noema:genesis:" || tenant_id)` and adds those three fields as a minor contract version. This package follows the PERD (higher in the order of authority, SoT §0.4). The contract change (AGENT-15: version bump, migration note, regenerated models) is not made here. See the CONFLICT block in the pull request.

**Tests.** `go test ./record/chain/...` from `services/core/`: a golden vector computed in Python, contiguity and tenant isolation, concurrent writers, a failed append rolling the action back, the stale-head refusal, eight kinds of tampering each detected with its break kind, the appending role's lack of UPDATE/DELETE, and refusal of events outside the contract. They need PostgreSQL server binaries or `NOEMA_TEST_DATABASE_URL`.
