# Status · EXE-RETRY-714 (Core Engineer 1) · bounded retry of a spend on a serialization failure

Updated 2026-11-18. Defect noema#714 (owner Execution: EL or C1 on the Architect's word). No plan task row: the work is the fix of that issue under REQ-EXE-001, REQ-EXE-002, INV-03, INV-07 and AGENT-16. Local commits only on branch `prep/714-serialization-retry`; not pushed, no pull request.

## State

- Done locally: `Ledger.Spend` runs the whole SERIALIZABLE transaction again, as a fresh transaction, when the database refuses it with SQLSTATE 40001 (`execution/ledger/retry.go`). Tests came first (red commit, then green).
- **Ruling.** Architect rulings A317 to A328 (relayed by the user; interim, not yet a committed decision record, so AGENTS.md section 2 item 3 applies: follow the committed documents if they disagree): 3 attempts in all; the number comes from the environment setting `NOEMA_DB_SERIALIZATION_ATTEMPTS=3`; no default in code; only 40001 is retried; each attempt is a fresh transaction; the context deadline bounds all attempts; after the last attempt the existing "did not commit" error.
- **The setting.** `NOEMA_DB_SERIALIZATION_ATTEMPTS`, read with `ledger.ParseSerializationAttempts` (a whole number of at least 1; empty, `0`, negative, signed, padded, leading-zero, non-numeric and out-of-range values are errors that name the setting). `Ledger.SerializationAttempts` below 1 makes `Spend` refuse before it opens a transaction (the `errNoRollbackBound` pattern).
- **Not wired in production.** `Ledger` is built only in tests on `main` (`cmd/core` does not construct it), so there is no production composition to wire. The code that builds it must read the setting with `ledger.EnvSerializationAttempts` and `ParseSerializationAttempts` and refuse to start on the error.

## For IL (protected paths, not touched here)

Add `NOEMA_DB_SERIALIZATION_ATTEMPTS=3` to the core service's environment in `deploy/` (compose and any env template) when the composition root that builds the `Ledger` lands. Until then no deployed process builds a `Ledger`, so nothing reads it. A missing value must stop the process at start.

## Open (Architect)

1. **Three attempts absorb the contention of one dispatch key, not of many distinct keys.** For one key the budget (2) caps the commits that can make a spend fail (two), so the third attempt sees a spent budget (the case of `TestTheTargetIsTheGuardWhenRevalidationSeesNothing`). For spends of distinct keys of one tenant, every round of contention on the chain head commits one spend and fails the rest; three attempts cannot serve eight of them. Such a caller still gets the 40001 "did not commit" error with its grant `ISSUED`. Not measured at scale; reasoned from the head lock. Question: is that acceptable as interim, or is a larger bound, or a caller-side retry, wanted?
2. The existing e2e test's `runAll` (`execution/e2e/records_test.go`) already presents a refused spend again up to 200 times with a pause, matching the typed error. It is unchanged as instructed; with the ledger retry it should seldom need it. Whether it should be reduced to prove the ledger alone is for the owner.
3. No pause between attempts (a pause would be a second retry parameter). Jitter or backoff is not in the ruling.

## Tests

Red commit `de5bc8a0` (fails on behaviour: no retry, no refusal without a value, parser stub); green commit after it. See the ledger README, "The retry on a serialization failure", for the list and what each proves. Mutants run against the new tests: retry on all errors, no retry, one attempt too many, one attempt too few, retry inside the same transaction, deadline ignored, no validation of the attempts, parser accepting zero, deadlock retried too: all killed. Dropping only the explicit `ctx.Err()` check survives (the next `BeginTx` on a finished context fails with a non-40001 error and ends the loop anyway; only the wording of the returned error differs).

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
