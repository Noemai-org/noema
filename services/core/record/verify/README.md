# record/verify

The chain verification job. Created by T1 of DEC-2026-10-20; slice 1 built by REC-05 (2026-11-03).

**Implements:** REQ-AUD-005 (nightly and on-demand chain verification), REQ-AUD-001. SoT IDs: INV-11, INV-10.

## What it does

- `Verifier.Run(ctx)` verifies every tenant that has a tenant row, a chain head or an event, in tenant order. `Verifier.RunTenant(ctx, id)` is the on-demand form for one tenant.
- Per tenant it opens one read-only REPEATABLE READ snapshot, so an append that lands meanwhile cannot look like a break. In that snapshot it:
  1. calls `chain.Verify`, which recomputes every hash from genesis, the projection columns and every stored payload hash;
  2. checks every event's signature (REC-08, `signatures.go`): the signed bytes are the RFC 8785 bytes of the stored body, the key is registered and belongs to the emitter that wrote the event, and the Ed25519 signature verifies over `noema/record-event/v1\n` + those bytes. It runs in Go with the public keys of `signing_key` and does not call the database's own verifier, so an event written while that trigger was off is still found (`SIGNATURE`). A retired key still verifies what it signed. An event signed under a revoked key at or after `compromised_since` is reported as `UNTRUSTED_KEY` for review, never accepted silently;
  3. checks that `record_chain_head` is the newest event (`HEAD_MISMATCH` otherwise). `chain.Verify` cannot see events dropped from the tail, because a shorter chain still verifies.
- On a break, in this order: count it (`noema_record_chain_break_total{kind}`), turn the tenant's kill switch on (E6 stops every write), raise the alert. If the alert cannot be delivered the tenant is already stopped and the failure is returned.
- A tenant that cannot be read is `ERROR`: not clean, not a break, nothing stopped on a guess. The run returns an error.
- A break in one tenant does not stop the run; the other tenants are verified.
- It never repairs a chain. A context without a deadline is refused.

## What it does not do yet

- **Signed, exported checkpoints** (REC-07): a rewrite that is consistent in itself, including the head row, is not detected until chain heads are anchored outside Postgres. The Record is tamper-evident, not tamper-proof.
- **The endpoint.** `RunTenant` is the function behind it; EDG-01 wires it behind the ops and audit role check, and no endpoint is wired before DEBT-04 option A lands.
- **The database role.** The job needs read access to `record_event`, `record_chain_head`, `tenant` and `signing_key` (migration 0019), and `UPDATE (kill_switch)` on `tenant`. Today the tests run as the administrator. A dedicated login role is a migration, and it waits for noema#107's `0003` to merge so the numbers do not collide.
- **Scheduling** (nightly and before every release run): REC-06 and the orchestrator call `Run`.
- **Replay** of each decision from its recorded inputs (REQ-AUD-005): a later slice.

## Tests

```
cd services/core && go test -race -count=1 ./record/verify
```

`verify_test.go` runs against a real PostgreSQL with the administrator tampering past the immutability trigger: eight kinds of tampering (edited body, edited hash, broken prior hash, deleted event, edited payload, newest events dropped, head row removed, head moved back) are each detected, alerted, counted and stop only that tenant; `signatures_test.go` adds seven signature faults (changed value, wrong length, another identity's key, an unregistered key, signed bytes that are not the stored event or not JSON, another event's signature), each a `SIGNATURE` break at the event, and the key-state cases (retired passes; revoked: later events reported, earlier ones trusted); a failed alert is an error and the tenant stays stopped; an unreadable tenant is ERROR; the job never changes a row.
