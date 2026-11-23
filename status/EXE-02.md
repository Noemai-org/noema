# Status · EXE-02 (Session A) · spend-and-dispatch controller

Updated 2026-10-25. Renamed from `EXE-01.md`: the plan's EXE-01 is the registry loader; the controller (E1 to E11) is plan task EXE-02. The merged commit message still says "EXE-01" and cannot be changed.

## State
- Merged (#47): the controller (E1-E11), crash-point injection, a 10,000-seed simulation and the D8 retry policy. ENV-08 mock-case reconciliation merged in #46.

## Open
- Real acceptance needs the approved Postgres driver and Temporal SDK (AGENT-14); not approved yet.
- The cost source for benchmark runs is Session D's harness (per-call cost files under `$BENCH_OUT/cost/`); nothing yet passes `BENCH_OUT`.

## Messages

### 2026-10-21 18:35Z sent · Session A → Session C
- Purpose: on the Founder's written instruction: the per-call cost source is Session D's model harness.
- Receiver used it for: not recorded.

## Session 2026-11-03 · EXE-02 (Core Engineer 1) · no panic on entropy failure

### Session summary
- What changed (PR links): the entropy-failure PR on `fix/EXE-02-no-panic-on-entropy-failure`. `ids.Generator.Must` is deleted; `ReceiptID()` returns `(string, error)`; the controller mints the receipt ID before E8, so a failing source refuses the presentation (`ErrIDSource`) with the grant unspent and nothing dispatched; the sweeper returns the error instead of panicking.
- Why: the Architect's instruction of 3 Nov. A panic on an entropy failure would take the process down mid-run.
- Risks, and how they are contained: the controller's `IDs` interface changed shape (an error return). The held noema#94 and #90 rewrite the ledger and merge `main` later; `ledger.IDSource` already returns an error and is untouched.
- Tests added (INV-07, INV-03): `TestAFailedIDSourceRefusesBeforeSpendAndDispatch` (no GRANT_SPENT, DISPATCH_ATTEMPTED or RECEIPT; zero effects; the unspent grant runs after the source recovers), `TestAnEntropyFailureIsAnErrorNotAPanic`, `TestThePackageHasNoPanic` (a source scan).
- Regression cases added: the three above. Mutation: ignoring the error fails the controller test.
- Debt entries: none.
- Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: none for this change.
- Do not touch: noema#94, noema#90 (held, DEBT-01).

## Session 2026-11-23 14:45 WAT · EXE-02 (Repo Steward) · the deadline test counted a statement in flight

### Session summary
- What changed (PR links): `services/core/execution/ledger/retry_test.go` only (the fix PR for `TestTheContextDeadlineStopsTheAttempts`).
- Why: `core` was red on `main` at `0fee9db` (noema#895, which does not touch the ledger): `retry_test.go:354: attempts went on after Spend returned: 8, then 9`. The previous eight `core` runs on `main` passed, and the ledger had not changed, so the test is intermittent. `spendWithRetry` starts no attempt once the context is done (`ctx.Err()` is checked after each failure), and the probe counts server-side, in the trigger, at the start of each insert. The most likely cause, consistent with the evidence and not reproduced (no pgsodium database here; CI is the proof): a statement the client sent just before the deadline began on a loaded server after `Spend` returned, so the counter moved by one.
- The fix: the test waits until no statement of the tenant's execution login is running before it reads the count, then keeps the same 300 ms check that nothing more is attempted. A client that really kept retrying would still start new statements after that point and fail the same assertion. No test is skipped, deleted or weakened in what it catches.
- Risks, and how they are contained: test-only change; the production retry code is untouched.
- Tests added (INV-07, AGENT-12): `waitForNoRunningStatement` helper used by the existing test.
- Regression cases added: the existing test, made deterministic.
- Debt entries: none.
- Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: if the test fails again on `main` after this merges, the cause is not the in-flight statement; read the log for the attempt counts and reopen.
- Do not touch: `services/core/execution/ledger/retry.go` (no attempt starts once the context is done).
