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

# Session 2026-11-19 12:00 WAT · EXE-RETRY-714 (A347 and A348 on noema#748)

## Session summary
- What changed (PR links): local commits on `prep/r6-748` (from `origin/fix/EXE-02-spend-serialization-retry`, main merged without conflict), not pushed. New `execution/ledger/errors.go` (`fixedError`, `publicSpendError`), `spend.go`, `retry.go`, `lock.go`, `ledger.go`; tests `errtext_test.go` (new), `spendlock_test.go` (release suite), `retry_test.go` (one helper), ledger README, this file.
- Why: A347 (relayed, interim): an error that crosses a package's public boundary has a fixed public text, the cause behind `Unwrap`. A348 (relayed; only the words "conditions on #748 (release test)" were available): the spend lock shown released on every path.
- A347, what is fixed. Every error that leaves `Spend`: no tenant, grant, key or nonce, no SQLSTATE, `ERROR:` or `FATAL` text, no server or login details; `errors.Is` and `errors.As` still reach the sentinels, `*pgconn.PgError` and the context's errors. `Spend` ends in `publicSpendError`, so a site that was missed gets a generic fixed text instead of leaking. The setting parser names the setting, never the value. The controller's `controller: spend: %w` adds no identifier (test). Tests that asserted on text now assert on the sentinel and the cause (`isDidNotCommit40001`); none deleted.
- A348, conditions as I read them (for the Architect to compare with the real text): after the call returns, with the real `pg_locks` of this database and `AcquiredConns` of the pool: commit; replay; rolled back by a fault that is not 40001; 40001 on every attempt; a context that expires while the transaction runs; a context already cancelled; a panic inside the spend; an unlock that fails (EXECUTE on `pg_advisory_unlock` revoked, so the connection is destroyed and the lock goes with the backend); a terminated backend (polled with a deadline); a spend that waited and got its turn leaves no waiter; 50 concurrent spends of 5 tenants end with no lock and no connection out. Not covered because the code has no such path: a failing `Release`, a lock timeout.
- DEFECT of #748 found: a panic inside the spend never released the connection or the tenant's advisory lock (`withSpendLock` had no deferred cleanup), so one panic stalled every later spend of that tenant until the process restarted. Red first (the suite's panic subtest), fixed by `runHeld`. All other paths already released.
- Risks, and how they are contained: `publicSpendError` wraps instead of replacing, so classification by `pgCode` and the controller's `errors.Is` are unchanged (the whole of ledger, controller and e2e passes under `-race -count=2`).
- Tests added (REQ, INV, US or EC IDs): REQ-EXE-001, REQ-EXE-002, INV-03, INV-07, AGENT-13, AGENT-16; see the ledger README.
- Regression cases added: the panic release case; the fixed-text table (21 classes).
- Debt entries: none.
- Conflicts raised: none.

## Open (Architect), added this session
7. **Unlock fails after a commit.** The spend is committed, the connection is destroyed (lock gone), and `Spend` returns the lock error with `Spent` false, so the controller does not dispatch: the grant is SPENT with an attempt and no receipt, which the sweeper turns into an unknown outcome. That is the code as it was written in A339; it is safe (nothing is dispatched) but a clean success becomes an error. Should a committed spend whose unlock failed return its result (the connection is closed either way)? I did not change it. The release test accepts either answer and pins only the release.
8. **Fifty spends of five tenants.** In one run 33 of 50 committed and 17 got the typed retryable error after three attempts, with each tenant's spends ordered by its lock: SERIALIZABLE transactions of different tenants still refuse one another ("read/write dependencies", on the Record's `lock head`, the insert, the mark-spent and the commit). Release is unaffected (no lock, no connection left). Whether three attempts is enough across tenants, or the Record's reads should not conflict across tenants, is for the Architect (AGENT-16: not mine to change). The test accepts spent or typed-retryable and states the count in its log.
9. **Other ledger errors.** `Load`, `Void`, `Complete`, `Append`, the binder and `chain.Appender` still print identifiers and driver text (`ledger: grant %s does not exist for tenant %s` in `Load` among them). They are outside #748; a sweep of them needs its own task. Also, `inTx` for those methods has no cleanup for a panic either (same shape as the defect fixed in `withSpendLock`).

## Pre-work for the next agent
- Exact next step: Architect answers open items 7 to 9; whoever owns the other ledger methods sweeps their errors the same way (`fixed`, never a message assertion in a test).
- Files to read first: `execution/ledger/errors.go`, `errtext_test.go`, the release suite in `spendlock_test.go`.
- Do not touch: `deploy/`, `.github/`.

### 2026-11-19 sent/accepted · user → Core Engineer 1 (relayed Architect rulings A347, A348)
- Purpose: A347 fixed-text errors sweep (interim); A348 conditions on #748 (release test).
- Receiver used it for: the fixed texts, the release suite, the panic fix.
- Evidence (PR or commit): local commits on `prep/r6-748`, not pushed.

# Session 2026-11-19 17:00 WAT · EXE-RETRY-714 (A348 refined: a failed unlock never changes the answer, noema#748)

## Session summary
- What changed (PR links): local commits on `prep/r6-748` (main merged, no conflict), not pushed. `execution/ledger/lock.go` (one deferred release; `runHeld` removed), `ledger.go` (optional `Log *slog.Logger`), tests `unlockfail_test.go` (new), `execution/e2e/unlockfail_dispatch_test.go` (new), `spendlock_test.go` (one subtest), ledger README, this file.
- Why: the refined ruling of A348, relayed by the user, interim, not committed (AGENTS.md section 2 item 3): return the committed result on unlock failure, log fixed text, close the connection; three attempts are enough; release on the same dedicated connection in a defer on every path with a fresh bounded context.
- What it does now. The release is one `defer` of `withSpendLock`, run on the same connection with `context.WithoutCancel(ctx)` plus `RollbackTimeout`. If the unlock errors, returns false (nothing to release) or leaves the connection's transaction not idle, the connection is closed (never returned to the pool) and ONE line is logged. The answer of the spend is never changed by the release: committed result with no error; the spend's own error when it did not commit; a replay stays a replay; a panic continues. The controller needed no change: it dispatches on `Spent` true with no error (the e2e test shows the write reaches the mock-case once).
- **Open item 7 of the earlier session: closed by the ruling.** The unlock failing after a commit used to return the lock error with `Spent` false.
- The log line: `slog` WARN, message `ledger: the spend lock could not be released cleanly: the connection was closed`, one attribute `event=SPEND_LOCK_RELEASE_FAILED`. I left out `tenant_id`, `correlation_id` and every other id although PERD 44.2 lists them as log fields: the ruling says fixed text, and an id from a failed path is the thing A347 sweeps. If EL wants `correlation_id` on the line (an operator cannot tie the line to a request without it), that is a one-attribute change and a decision for EL.
- **For EL's composition root (nothing builds a `Ledger` in `cmd/core` yet):** set `Ledger.Log` to the process logger wrapped by `platform/redaction.Handler` (PERD 44.2 mandatory redactor); nil logs nothing, so an unwired root loses the line silently. No new dependency (`log/slog` is the standard library and is what `record/reconstruct` already uses), no global, no reflection. The existing wiring notes stay: `NOEMA_DB_SERIALIZATION_ATTEMPTS=3` and `RollbackTimeout` (no default).
- Three attempts stay unchanged (`SerializationAttempts` is the configured 3; no code change).
- Tests (REQ-EXE-001, REQ-EXE-002, INV-03, INV-07, AGENT-13, AGENT-16), real PostgreSQL 16 and the real mock-case, nothing mocked: unlock refused (EXECUTE on `pg_advisory_unlock` revoked), unlock that finds nothing (a trigger runs `pg_advisory_unlock_all()` in the spend), panic with and without a failing unlock, caller context cancelled by a pgx tracer right after the commit is acknowledged (with a working unlock and with a failing one), spend that did not commit (non-40001 fault; 40001 on every attempt; a replay), a Ledger with no logger, e2e dispatch. RED commits `efce2b65` and `6d6eff32` (eight ledger tests and the e2e test fail on assertions), GREEN `3dadfcc6`.
- **Test changed.** The subtest "the unlock itself fails" of `TestTheSpendLockIsReleasedOnEveryPath` accepted either answer (an error or none); it now requires the committed result and no error. Nothing else was weakened or deleted. One test of mine was corrected in the green commit (the next spend used a target whose dispatch budget was already spent; the 40001 subtest does not count the pool connection that the grant-status read after the lock keeps).
- Pin, not red: `TestTheUnlockRunsOnAFreshContextWhenTheCallersContextIsCancelledAfterTheCommit` passed before the change (the unlock already used a fresh context); it kills the mutant that uses the caller's context.
- Regression case: the tests above; the defect fixed in this session's scope is the changed answer on a failed unlock.
- Debt entries: none. Conflicts raised: none.

## Item 9 of the earlier session: what else in `execution/ledger` still returns identifiers or driver text (listed, not changed)
`Load` (`ledger: grant %s does not exist for tenant %s`, and the driver's error with `%w`), `Void`, `Complete`, `Append`, the binder methods (`WritesEnabled` and the others) and `inTxOn` itself (`fmt.Errorf("%sbegin: %w")` and `commit: %w` wrap the driver's error and its text; `Spend` hides them behind `publicSpendError`, the other methods do not), `needDeadline`'s callers, and `chain.Appender`'s errors (it names the tenant and the driver). `inTx` of those methods also has no cleanup for a panic inside the callback (the connection of a transaction is then returned by pgx's own handling only when the pool sees it non-idle; not measured). Not changed here.

## Pre-work for the next agent
- Exact next step: C1 verifies and pushes; EL wires `Ledger.Log` in the composition root; EL decides whether the line carries `correlation_id`.
- Files to read first: `execution/ledger/lock.go`, `unlockfail_test.go`, README "Release on every path".
- Do not touch: `deploy/`, `.github/`.

### 2026-11-19 sent/accepted · user → Core Engineer 1 (relayed refinement of A348)
- Purpose: return the committed result on unlock failure; one fixed log line; close the connection; release in a defer on the same connection with a fresh bounded context.
- Receiver used it for: lock.go, the tests above.
- Evidence (PR or commit): local commits `efce2b65`, `6d6eff32`, `3dadfcc6` and the docs commit on `prep/r6-748`; not pushed.
