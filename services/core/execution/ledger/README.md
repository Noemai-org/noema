# execution/ledger

**What it does.** The Postgres implementation of the controller's `Ledger` and `GrantSource` ports, plus the kill-switch read for `Binder.WritesEnabled`. Every method is one transaction run as the `execution_rw` role.

**REQs.** REQ-EXE-001 (single use by unique constraint on `grant_id` and `nonce`), REQ-EXE-002 (the key is committed before dispatch), REQ-EXE-006 (the sweeper's input), REQ-EXE-008 (the kill switch is read inside the spend), REQ-AUD-001 (Record events appended in the transaction of the object write; an append failure fails the step, EC-E16).

**How the steps map.**
- `Spend` is E8: SERIALIZABLE; checks the grant is `ISSUED`, reads the kill switch, upserts `idempotency_record` (attempt + 1; the same key with other parameters or another action is a collision, EC-E06), inserts `grant_spend` with the key and attempt, marks the grant `SPENT`, appends `GRANT_SPENT` and `DISPATCH_ATTEMPTED`. A grant that is no longer `ISSUED`, or that loses a race, is a replay. A collision raises `SECURITY_EVENT IDEMPOTENCY_COLLISION` and returns `controller.ErrKeyCollision`, which the controller answers by voiding the grant.
- `Void` marks the grant `VOID`, inserts one `recovery_request`, enqueues `recovery.requested` and appends `GRANT_VOIDED`, once per grant.
- `Complete` of a `REJECTED_STALE_VERSION` receipt (REQ-EXE-004, EXE-04; PERD v1.18 §38.2) also voids the spent grant in the same transaction, after `RECEIPT`: the grant moves `SPENT → VOID` (migration `0002` lets the trigger allow it only for a confirmed refusal), one `recovery_request` (reason `REJECTED_STALE_VERSION`), the `recovery.requested` event and `GRANT_VOIDED`, whose payload names `from_status` `SPENT`, `to_status` `VOID` and the receipt. `GRANT_SPENT` stays on the Record. If any of these cannot be written, none is, and the sweeper handles the attempt.
- `Complete` is E11: inserts the signed receipt (first writer wins), moves the idempotency record to `RECEIPTED`, enqueues `outcome.verify` and appends `RECEIPT`, in one transaction.
- `Dispatching` lists spent attempts with no receipt.

**Not here.** The `Binder` reads of the intent, the decision and the plan (Trust and the registry are not built), the `Revalidator`, the dispatcher and the sweeper's age threshold. The sweeper writes receipts for every unreceipted attempt it sees; a threshold so that it does not race a dispatch still in flight is configuration (REQ-OPS-003) and is not built.

**Tests.** `go test ./execution/ledger/...` from `services/core/`, against a real PostgreSQL (`internal/testpg`) with the real controller: a clean run in order, 100 concurrent executes of one grant (one dispatch), void and recovery request, kill switch flipped between E6 and E8, crash after the spend and the sweep, a failed Record append failing the spend, a retry under a new grant, key collision, first-writer-wins receipts, grant loading, deadlines.
