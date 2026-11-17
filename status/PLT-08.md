# Status · PLT-08 · Transactional outbox table and dispatcher

Updated 2026-11-17. Owner in `tasks.csv`: C1; taken over by Core Engineer 2 (C2) on the Founder's order of 17 Nov 2026. Plan task PLT-08 (REQ-VER-005; Needs PLT-06). Done when: "An event commits with its state change or not at all (crash test)". Gate: none of its own; PLT-08 feeds OUT-04 (inbound inbox, plan week 7) and the outbox consumers of OUT-03.

## State
- In progress. Red tests first (this pull request, draft). The implementation follows after the red CI run is read.

## Task card (PERD §61.4; the docs repository holds no card for PLT-08, so this is the card)
- Task: PLT-08. REQ: REQ-VER-005 (the row's REQ; see "Open questions" 1: its text is the `outcome.updated` callback, and the outbox rule that governs this task is PERD §56, "Outbox"). SoT: §35.3 ("transactional outbox for any event that must be emitted atomically with a state change; no lost or phantom events"), INV-11, INV-12, INV-30. PERD: §36.1 (transactional outbox, never dual writes), §38.2 (`outbox_event`: PENDING to DISPATCHED, mutable, any power writes, 30 d retention after dispatch), §56, REQ-AUD-001 and EC-E16 ("Record comes first").
- Allowlist (the card's, mine to propose; none of it is a protected path): `services/core/platform/outbox/**` (new), `services/core/migrations/0031_outbox_lifecycle.sql` (new), `services/core/platform/outbox/README.md`, `status/PLT-08.md`. Nothing in `execution/`, `outcome/`, `trust/`, `record/`, `.github/`, `deploy/`, `contracts/`, `packs/`.
- Acceptance: the row's "Done when" (an event commits with its state change or not at all, shown by a crash test) and, because the row says "and dispatcher": a dispatcher that delivers each event at least once with a stable idempotency key (the outbox ID, PERD §56) and marks it dispatched only after delivery.

## Investigation (read from `main` at `af7b6fde`, 17 Nov 2026)

What already exists:

| Piece | Where | Evidence |
|---|---|---|
| The table `outbox_event` (`outbox_id` PK, `tenant_id`, `topic`, `payload jsonb`, `created_at`, `dispatched_at` nullable) and the partial index `outbox_event_pending_idx (tenant_id, created_at) WHERE dispatched_at IS NULL` | `migrations/0001_execution_and_record.sql` lines 162 to 172 | read |
| Grants: `trust_rw`, `execution_rw`, `outcome_rw` have SELECT and INSERT; `orchestrator_rw` has SELECT and `UPDATE (dispatched_at)` ("the orchestrator's dispatcher marks outbox events dispatched"); nobody has DELETE | same file, lines 253 to 269 | read; confirmed on a local PostgreSQL 16: `execution_rw` cannot UPDATE or DELETE, `orchestrator_rw` cannot update the payload |
| Row-level security, FORCE, one policy per table keyed on the login's tenant (`decision_login_tenant()`), outbox included | `migrations/0018_row_security.sql` | read; `platform/db/rls_tables_test.go` pins it |
| Two producers, each a raw `INSERT INTO outbox_event` inside the state transaction: `execution/ledger/complete.go` (`outcome.verify`, `recovery.requested`) and `outcome/store/store.go` (`recovery.requested`) | `git grep -n -i outbox -- services/core` | read |
| Producer-side crash and atomicity tests: `execution/ledger/boundary_test.go` (a kill inside the receipt transaction leaves no receipt, no idempotency update, no outbox event, no RECEIPT event), `adapters/outcome_e2e/crash_test.go` (real SIGKILL, counts the `outcome.verify` outbox event), `outcome/store/flow_test.go` (EC-E16: a failed Record append leaves no verdict, request, watch or outbox event) | read | These need the Record, so they need `pgsodium`; they run in CI, not here |

What is missing against the done-when:

1. **No dispatcher.** `git grep` finds no code that reads a pending `outbox_event`, delivers it, or sets `dispatched_at`. Every `outbox_event` written today stays pending for ever. Only the grant (`orchestrator_rw`) exists.
2. **No general proof at the outbox level.** The crash tests above prove two producers, each through its whole domain transaction. None proves the outbox write itself: that an outbox row and any state change are all-or-nothing under a real process death, independent of the Record and of one producer's code. The producers also each carry their own copy of the INSERT, with no shared validation (ID shape, topic shape, payload is a JSON object) and no error that is safe to log.
3. **The lifecycle is not enforced.** PERD §38.2 gives the lifecycle PENDING to DISPATCHED. On a local PostgreSQL 16 I confirmed that (a) `execution_rw` can INSERT a row that is already dispatched (an event that will never be delivered, a lost event that commits), and (b) `orchestrator_rw` can set `dispatched_at` back to NULL (a redelivery nobody asked for). Both are allowed by the column grants alone.
4. **Not in scope here** (see "Not built"): the callback itself (HMAC signing, tenant webhook secret, 12-attempt backoff, `CALLBACK_*` Record events: REQ-VER-005 body, which depends on REQ-API-006), starting Temporal workflows from outbox entries (STM-04 and OUT-03 own the worker and the watch workflows), the scheduling loop in `cmd/core`, and moving the two producers onto the shared `Enqueue` (a refactor is its own PR, AGENTS section 4).

So the table, the grants and row security are built. The task is the dispatcher, a shared typed write path, the lifecycle guard, and the crash test at the outbox level.

## Plan

Files (all new, none edited after merge once merged):
- `services/core/platform/outbox/doc.go`, `event.go` (`Event`, `Enqueue(ctx, tx, Event)`), `dispatcher.go` (`Dispatcher`, `Handler`, `Delivery`, `Config`, `RunOnce`, `Result`, `DeliveryFailure`), `README.md`.
- `services/core/migrations/0031_outbox_lifecycle.sql`: one trigger function and two triggers on `outbox_event` (insert is pending; update moves `dispatched_at` from NULL to a value and nothing else; a dispatched row never changes). No new role, no new table, no new column.
- This file.

Design in four lines. (1) `Enqueue` takes the caller's `pgx.Tx`: it never begins, commits or rolls back, so an event and its state change are one transaction by construction (no dual write). (2) The dispatcher is one pass, `RunOnce`: per event, in its own transaction, `SELECT ... FOR UPDATE SKIP LOCKED` the oldest pending row of its tenant, call the topic's handler with the outbox ID as the idempotency key, then `UPDATE ... SET dispatched_at` and COMMIT. A handler error, a missing handler, a crash or a failed commit leaves the row pending, so it is delivered again: at least once. (3) It runs as the tenant's `orchestrator_rw` login, so row security bounds it to one tenant; it also filters by its configured tenant. (4) Errors name the outbox ID and the topic, never the payload; the cause stays reachable through `errors.Is`.

Tests first (REQ-VER-005, PERD §56, EC-E16, INV-11, INV-12, INV-30), each seen failing before the implementation (the package does not exist yet, so they do not compile):
- Crash (real SIGKILL of a child process, the pattern of `adapters/outcome_e2e/crash_test.go`; no production hook is added): a state row and `Enqueue` in one transaction, killed before COMMIT: neither exists. Killed after COMMIT: both exist, once. A failure of the state change after `Enqueue` rolls back both. `Enqueue` on a rolled-back transaction leaves nothing.
- Dispatcher crash: a handler that has delivered (recorded in a file) and then kills its own process before the mark: the event is still pending; a new dispatcher delivers it again with the same outbox ID and marks it; a further pass delivers nothing.
- Ordering of mark and delivery: at the moment the handler runs, a separate connection sees `dispatched_at` NULL; after `RunOnce` it is set, to the injected clock's instant.
- A failing handler and a topic with no handler leave the event pending and do not stop the other events; the failure is reported without the payload.
- Two dispatchers at once deliver every event exactly once when nothing crashes; a second dispatcher skips a row the first still holds.
- Tenant isolation: a dispatcher on tenant A's login never delivers tenant B's event, and one configured for the wrong tenant delivers nothing.
- Lifecycle trigger: a born-dispatched insert, an un-dispatch, a second dispatch and an edit of the payload, topic, tenant, ID or `created_at` are each refused, for the powers and for the owner; the legal mark is allowed.
- Pure unit tests (they run without a database): event validation (ID shape, topic shape, payload a JSON object, tenant, time), config validation (no defaults for the budget and the timeout), no payload in any error text, `Enqueue` never calls Begin, Commit or Rollback (AST check).

Faults exercised: process death (SIGKILL) inside a transaction and inside a delivery; handler error; no handler; concurrent dispatchers; a lost commit is the same as the crash after delivery. Faults come from the real process and the real database, not mocks (AGENT-12).

Record events asserted: none. The outbox is not the Record. The outbox holds no Record append and `Enqueue` writes no Record event; the producers append theirs in the same transaction (asserted by their own tests). `CALLBACK_*` events belong to the callback task (open question 1).

## Local limits
- The sandbox PostgreSQL 16 has no `pgsodium`: any test that appends a Record event fails here with `schema "pgsodium" does not exist`. The new tests append none, so they run here. The existing producer crash tests (`execution/ledger`, `adapters/outcome_e2e`) cannot run here; they run in CI.

## Open questions (listed, not decided)
1. The row cites REQ-VER-005, whose text is the `outcome.updated` callback (HMAC signature, per-tenant webhook secret, backoff 1 s doubling to 10 min over at most 12 attempts, `CALLBACK_*` events, depends on REQ-API-006). The done-when and the outbox rule (PERD §56) do not need any of that. I read the row as the outbox part of REQ-VER-005 and built none of the callback. Owner: Engineering Lead or the Architect to confirm, or to split the row.
2. Retry policy of the general dispatcher. PERD §56 says "at least once"; the 12-attempt backoff is written for the callback only. Without a decision I add no attempt counter, no backoff column and no dead-letter state: a failing event is retried on every pass. A cap and backoff need columns (`attempt`, `next_attempt_at`), a migration and a number. Owner: Architect.
3. Who calls `RunOnce`, and how often (a Temporal activity, a sweeper, a timer in `cmd/core`). Not built; the interval is configuration with no default. PERD §56: "Workflows are started from outbox entries ... On a Temporal outage, sweepers drive the same activities from Postgres state". Owner: STM-04 (worker) and C1.
4. Package location `services/core/platform/outbox`. PERD §64.2 lists `platform/` as `db, clock, crypto, config, telemetry, redaction` and does not name an outbox package. Smallest choice made; Owner: Engineering Lead.
5. Migration number. The ledger (noema#183, C1's table of 17 Nov) has 0029 and 0030 assigned and says "next free: 0031". I use `0031` provisionally; the number is the Repo Steward's to confirm, and a later PR renumbers if the merge order changes (ruling A34). No open pull request changes `services/core/migrations/` (checked 17 Nov).
6. PLT-06 is "In progress" in the docs `tasks.csv` (Needs column of PLT-08). Its migrations and roles are on `main`; whether the Steward counts it Done is theirs.
7. The two producers still hold their own `INSERT INTO outbox_event`. Moving them to `Enqueue` is a refactor PR (own task ID, no behaviour change). Not done here.
8. Retention (30 d after dispatch, PERD §38.2) needs a DELETE nobody holds; it belongs to DATA-01.
9. The lifecycle trigger (migration 0031) is my addition, justified by the two holes in "What is missing", item 3. It is Tier 1 (migration). If the reviewers prefer not to add a migration for PLT-08, the dispatcher and `Enqueue` stand without it and only the lifecycle tests drop out. Owner: Security Reviewer.
