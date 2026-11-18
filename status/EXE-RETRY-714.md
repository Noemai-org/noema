# Status · EXE-RETRY-714 (Core Engineer 1) · bounded retry of a spend on a serialization failure

Updated 2026-11-18 (second session: ruling A339). Defect noema#714 (owner Execution: EL or C1 on the Architect's word). No plan task row: the work is the fix of that issue under REQ-EXE-001, REQ-EXE-002, INV-03, INV-07 and AGENT-16. Local commits only on branch `prep/714-serialization-retry`; not pushed, no pull request.

## State

- Done locally: `Ledger.Spend` runs the whole SERIALIZABLE transaction again, as a fresh transaction, when the database refuses it with SQLSTATE 40001 (`execution/ledger/retry.go`). Tests came first (red commit, then green).
- **Ruling.** Architect rulings A317 to A328 (relayed by the user; interim, not yet a committed decision record, so AGENTS.md section 2 item 3 applies: follow the committed documents if they disagree): 3 attempts in all; the number comes from the environment setting `NOEMA_DB_SERIALIZATION_ATTEMPTS=3`; no default in code; only 40001 is retried; each attempt is a fresh transaction; the context deadline bounds all attempts; after the last attempt the existing "did not commit" error.
- **Ruling A339** (the user's order of 18 Nov, relayed, not committed: "advisory lock + typed retryable error"). Two additions on top of the retry, both local commits after the first three: `Spend` runs on one connection that holds a per-tenant advisory lock, so the spends of one tenant take turns on the chain head; and a `40001` that persists after the last attempt is `controller.ErrSpendRetryable` around the database's error (code `DEPENDENCY_UNAVAILABLE` of `errors.v2.json`, no new code). Detail in the ledger README.
- **Deviation from the wording of A339, for the Architect to confirm.** A339 as relayed says a transaction-scoped lock (`pg_advisory_xact_lock`) as the first statement of the spend transaction. That does not serialise anything on this database: a SERIALIZABLE transaction takes its snapshot at its first statement, the lock call included, so a waiter granted the lock after the previous spend committed still holds the old snapshot and is refused with `40001` on its first write to the chain head (measured: `TestAnAdvisoryXactLockAsTheFirstStatementOfASerializableTransactionDoesNotRefreshItsSnapshot`; the mutant that takes the xact lock first fails the eight-key test). So the lock is a **session** advisory lock taken on the connection **before** `BEGIN`, released (or the connection destroyed) before the connection returns to the pool. The intent of A339 (the spends of one tenant serialize on the head) is met; the literal mechanism is not. If the Architect wants the literal one anyway, it needs a different isolation level or a lock taken in a separate prior transaction, which changes the single-use argument.
- **The setting.** `NOEMA_DB_SERIALIZATION_ATTEMPTS`, read with `ledger.ParseSerializationAttempts` (a whole number of at least 1; empty, `0`, negative, signed, padded, leading-zero, non-numeric and out-of-range values are errors that name the setting). `Ledger.SerializationAttempts` below 1 makes `Spend` refuse before it opens a transaction (the `errNoRollbackBound` pattern).
- **Not wired in production.** `Ledger` is built only in tests on `main` (`cmd/core` does not construct it), so there is no production composition to wire. The code that builds it must read the setting with `ledger.EnvSerializationAttempts` and `ParseSerializationAttempts` and refuse to start on the error.

## For IL (protected paths, not touched here)

Add `NOEMA_DB_SERIALIZATION_ATTEMPTS=3` to the core service's environment in `deploy/` (compose and any env template) when the composition root that builds the `Ledger` lands. Until then no deployed process builds a `Ledger`, so nothing reads it. A missing value must stop the process at start.

## Open (Architect)

1. **Replaced by A339.** The first version of this file asked whether three attempts were enough for spends of many distinct keys of one tenant. With the per-tenant lock the spends of one tenant no longer race each other: eight distinct keys, three attempts, all spend (three runs, spends made slow so they overlap). What remains for `40001` is a writer that does not take the lock (Trust's appends, `Append`, `Complete`) landing on the chain head under a spend; the retry covers it, and the reproduction of noema#714 with one attempt now refuses exactly the one spend that held the lock instead of every queued one. If that still persists after three attempts the caller gets the typed retryable error, not a refusal. Not measured at scale (no load test was run); reasoned and tested on a real PostgreSQL.
2. **Confirm the session lock** instead of the literal transaction-scoped one (State, above).
3. **The pool.** A spend waiting for its tenant's turn holds one pool connection. A burst of spends of one tenant larger than the pool leaves the other queries of the process waiting for a connection until the burst drains. The holder needs no other connection, so there is no deadlock. Pool size is not changed here (AGENT-16: not mine to set); whether it is a concern is for the owner.
4. The existing e2e test's `runAll` (`execution/e2e/records_test.go`) already presents a refused spend again up to 200 times with a pause, matching the database's code through the error chain (`errors.As` to `*pgconn.PgError`), which the typed error keeps. It is unchanged as instructed; with the lock and the retry it should seldom need it. The controller needed no change for the typed error: `Execute` already wraps the `Spend` error with `%w`; the mapping is now held by tests (`controller/retryable_test.go`).
5. No pause between attempts (a pause would be a second retry parameter). Jitter or backoff is not in the ruling.
6. **Callers of `Execute`.** `execution/bridge` (`GrantExecutor`) returns the error as it is; `orchestrator/portadapters/executor` wraps it with `%w`; `orchestrator/runwrite.go` (`execute`) stops the run on any Executor error, on the ground that "the write may have been dispatched, and only the Record can say". For `controller.ErrSpendRetryable` that ground is false (nothing was dispatched, the grant is `ISSUED`), so the orchestrator could present the grant again. I did not change them: they are other modules and another task's allowlist, and the typed error reaches them intact through `errors.Is`. No caller maps the error to an HTTP body in this tree; when one does it should report `DEPENDENCY_UNAVAILABLE` with `retryable: true` (`controller.CodeSpendRetryable`). Whose task it is to teach the orchestrator is for the Architect.

## Tests

Layer 2 (first session): red commit `de5bc8a0`, green `a28daf41`. Mutants run against its tests: retry on all errors, no retry, one attempt too many, one attempt too few, retry inside the same transaction, deadline ignored, no validation of the attempts, parser accepting zero, deadlock retried too: all killed. Dropping only the explicit `ctx.Err()` check survives (the next `BeginTx` on a finished context fails with a non-40001 error and ends the loop anyway; only the wording of the returned error differs).

A339 (second session): red commit `6dca1160` (nine tests fail, for the right reasons: eight distinct keys still refuse one another with 40001, no lock is held or awaited, a persistent 40001 is untyped), green `d981d58f`. One earlier test changed expectation, not strength: the reproduction of noema#714 expected at least as many refusals as queued spends; with the lock it expects exactly one refusal and seven spends (`TestWithOneAttemptOnlyTheSpendWhoseTurnItIsLosesTheHeadToAWriterOutsideTheLock`, the old name's test). Mutants against the new tests, all killed: no lock; lock on a constant (not per tenant); transaction-scoped lock as the first statement of the transaction (the literal ruling); transaction-scoped lock after another statement; one-key lock form; unlock missing (the lock leaks to the pool: spends hang and fail); lock wait ignoring the context; typed error missing; typed error for every did-not-commit error; typed error without the database cause; controller wrapping with `%v`.

# Session 2026-11-18 16:13 WAT · EXE-RETRY-714

## Session summary
- What changed (PR links): local branch `prep/714-serialization-retry`, no PR yet. `execution/ledger/retry.go` (new), `spend.go`, `ledger.go` (field), `retry_test.go` (new), `world_test.go`, `execution/e2e/stack_test.go`, `adapters/outcome_e2e/{stack,crash}_test.go` (the stacks that build a `Ledger` set `SerializationAttempts: 3`), ledger README, this file.
- Why: a valid unspent grant lost a Record chain head race and was refused to its presenter with 40001 (noema#714).
- Risks, and how they are contained: a retry never repeats a dispatch (nothing is dispatched inside `Spend`; a 40001 rolled the transaction back, so no spend, receipt or Record event exists), and single use is still enforced by `grant_spend` unique constraints and the first statement of each attempt (INV-03, AGENT-08 is not engaged: no grant is presented twice to a target). Every attempt re-reads the grant. The bound is configuration with no default.
- Tests added (REQ, INV, US or EC IDs): REQ-EXE-001, REQ-EXE-002, INV-03, INV-07, AGENT-16; see `retry_test.go`.
- Regression cases added: the forced-contention tests and the exact-count tests above.
- Debt entries: none.
- Conflicts raised: none. The ruling is relayed and not committed (see State).

## Pre-work for the next agent
- Exact next step: IL adds the setting to `deploy/`; whoever builds the `Ledger` in `cmd/core` reads it with `ParseSerializationAttempts`. Architect answers Open item 1.
- Open questions (owner): Open items 1 to 3 above (Architect).
- Files to read first: `services/core/execution/ledger/retry.go`, `retry_test.go`, README section "The retry on a serialization failure".
- Do not touch: `deploy/`, `.github/`, the existing e2e test `TestTheTargetIsTheGuardWhenRevalidationSeesNothing`.

## Messages

### 2026-11-18 sent/accepted · user → Core Engineer 1 (relayed Architect ruling)
- Purpose: A317 to A328 (interim): bounded retry of the whole spend transaction on 40001, 3 attempts, `NOEMA_DB_SERIALIZATION_ATTEMPTS`, no default.
- Receiver used it for: this change.
- Evidence (PR or commit): local commits on `prep/714-serialization-retry`; not pushed.

# Session 2026-11-18 19:20 WAT · EXE-RETRY-714 (A339)

## Session summary
- What changed (PR links): local branch `prep/714-serialization-retry`, no PR yet. New `execution/ledger/lock.go` (`withSpendLock`, the per-tenant session lock on a held connection), `retry.go` (the retry runs inside the lock), `ledger.go` (`inTxOn`, `SpendLockNamespace`, `SpendLockName`), `spend.go` (typed error), `controller/types.go` (`ErrSpendRetryable`, `CodeSpendRetryable`), tests `ledger/spendlock_test.go`, `controller/retryable_test.go`, `ledger/retry_test.go` (one test re-expressed), ledger README, this file.
- Why: relayed ruling A339, "advisory lock + typed retryable error" (open item 1 of the first session).
- Risks, and how they are contained: a leaked session lock would stall every later spend of a tenant; the lock is released before the connection returns, the connection is destroyed if that cannot be shown, and a mutant that skips the unlock is killed. A waiting spend holds a pool connection (open item 3). Single use is unchanged: the lock adds ordering, the unique constraints and the first statement of each attempt still decide (INV-03). A spend still never dispatches inside `Spend` (AGENT-08 not engaged).
- Tests added (REQ, INV, US or EC IDs): REQ-EXE-001, REQ-EXE-002, INV-03, INV-07, AGENT-16; see the ledger README, "Tests".
- Regression cases added: the eight-distinct-key test, the head-race reproduction, the typed-error tests.
- Debt entries: none.
- Conflicts raised: none. A deviation from the relayed wording of A339 is stated above (session lock, not transaction-scoped), with its evidence.

## Pre-work for the next agent
- Exact next step: Architect confirms the session lock (open item 2) and answers open item 3; IL adds `NOEMA_DB_SERIALIZATION_ATTEMPTS=3` to `deploy/` when the composition root builds the `Ledger`; whoever maps `Execute`'s error at the edge uses `controller.ErrSpendRetryable` (open item 6).
- Files to read first: `execution/ledger/lock.go`, `spendlock_test.go`, README "The spend lock and the retry".
- Do not touch: `deploy/`, `.github/`, `TestTheTargetIsTheGuardWhenRevalidationSeesNothing` and `runAll` in `execution/e2e/records_test.go`.

### 2026-11-18 sent/accepted · user → Core Engineer 1 (relayed Architect ruling A339)
- Purpose: "advisory lock + typed retryable error": per-tenant lock as the first statement of the spend transaction, typed retryable error after the last attempt.
- Receiver used it for: the lock (as a session lock, see State), the typed error, the tests above.
- Evidence (PR or commit): local commits `6dca1160` (red), `d981d58f` (green) on `prep/714-serialization-retry`; not pushed.
