# platform/outbox

**What it does.** The transactional outbox of PERD §56: `Enqueue` writes an event on the caller's own transaction, so it commits with the state change or not at all; `Dispatcher.RunOnce` delivers pending events at least once and marks each dispatched only after its delivery.

**REQs.** REQ-VER-005 (the outbox part: "delivery is at least once, from the outbox"), PERD §56, §36.1 (transactional outbox, never dual writes), §38.2 (`outbox_event`), EC-E16. Task PLT-08. SoT §35.3, INV-11, INV-12, INV-30.

## Built

| Piece | Rule |
|---|---|
| `Enqueue(ctx, tx, Event)` | Joins the caller's `pgx.Tx`; never begins, commits or rolls back (checked by an AST test). Validates the outbox ID (`obx_<ULID>`), tenant, topic (dotted lower-case words), a JSON-object payload and a creation time before it touches the database. A database refusal is returned as its SQLSTATE and constraint name only: a message may quote the row, and the row holds the payload. A duplicate ID is `ErrDuplicateEvent`. |
| `Dispatcher.RunOnce(ctx)` | One bounded pass over one tenant's pending events, oldest first. Per event, in its own transaction: `SELECT ... FOR UPDATE SKIP LOCKED`, call the topic's `Handler` with the `Delivery` (its `IdempotencyKey()` is the outbox ID), and only if the handler returned nil, set `dispatched_at` (from the injected clock) and commit. A handler error, a topic with no handler (`ErrNoHandler`), a handler past `DeliveryTimeout`, a death of the process or a failed commit leave the event pending, so it is delivered again with the same key: at least once. A failed event is tried once per pass. Two dispatchers never hold one event. |
| Errors | `RunOnce` returns an error wrapping `ErrDeliveryFailed` when an event failed; `Result.Failed` lists them. A `DeliveryFailure`'s text names the outbox ID and the topic and never the cause's text or the payload; `errors.Is` still reaches the cause. |
| Configuration | `Config{TenantID, Handlers, MaxEvents, DeliveryTimeout}`: no defaults (REQ-AGT-005, AGENT-16). The dispatcher runs on the tenant's own `orchestrator_rw` login; row security (0018) bounds it to that tenant, and the query filters by it too. |
| Migration `0031_outbox_lifecycle.sql` (number proposed; Tier 1) | A trigger makes the lifecycle one way for every role including the owner: an event is created pending; its only change is `dispatched_at` moving once from NULL to a value, the rest unchanged; a dispatched event never changes. Closes two holes of the 0001 grants (a producer inserting a born-dispatched event, a lost event; the dispatcher setting the mark back to NULL). DELETE is untouched (retention is DATA-01). No role, table or column. |

## Not built

- The callback of REQ-VER-005 (HMAC signature, per-tenant webhook secret, 1 s doubling to 10 min over at most 12 attempts, `CALLBACK_*` Record events, `noema_callback_delivery_failures_total`): it depends on REQ-API-006.
- Any retry counter, backoff or dead-letter state for the general dispatcher: undecided (open question 2 in `status/PLT-08.md`); a failing event is retried on every pass.
- The scheduler that calls `RunOnce`, the Temporal start from an outbox entry and the sweeper fallback (STM-04, OUT-03; PERD §56).
- Moving `execution/ledger` and `outcome/store` from their own `INSERT INTO outbox_event` to `Enqueue`: a refactor, its own PR.
- The 30-day retention DELETE (DATA-01).

## Tests

`go test ./platform/outbox/...` from `services/core/`. The unit tests need no database. The rest start a throwaway PostgreSQL server (`internal/testpg`), or use `NOEMA_TEST_DATABASE_URL`. `crash_test.go` (Linux only) runs the test binary again as a child that connects through the tenant's own logins and kills it with SIGKILL at a named point: after the state change and the event and before COMMIT (neither exists), after COMMIT (both exist), and after a delivery and before its mark (the event is delivered again with the same ID, and the mark of the earlier event of the pass stands). No production code carries a crash hook. The tests append no Record event, so they run without `pgsodium`. The state change is a row of a synthetic test table; the two real producers keep their own crash tests (`execution/ledger/boundary_test.go`, `adapters/outcome_e2e/crash_test.go`), which need `pgsodium` and run in CI. All data is SYNTHETIC.
