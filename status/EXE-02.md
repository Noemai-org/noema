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
- What changed (PR links): `services/core/execution/ledger/retry_test.go` only (noema#915).
- Why: `core` was red on `main` at `0fee9db` (noema#895, which does not touch the ledger): `retry_test.go:354: attempts went on after Spend returned: 8, then 9`. The previous eight `core` runs on `main` passed and the ledger had not changed, so the test is intermittent; one re-run passed. `spendWithRetry` starts no attempt once the context is done. The probe counts server-side, in the trigger, at the start of each insert. The count moved by exactly one, which is what one statement already sent but not yet begun at the server would do under load. Not reproduced: 40 runs of the old test under 12 busy loops on 4 cores passed. So the cause is the explanation that fits, not a proven one.
- The fix: after `Spend` returns, the test takes the count, ends every server-side connection of the tenant's execution login (`endTheLoginsBackends`, which polls and does not read the clock), allows the count to have moved by at most one (the one statement a single connection can have in flight), then keeps the 300 ms check that nothing more is attempted. A first version only waited for no running statement; a mutant showed that does not cover a statement sent and not yet begun, so it was replaced.
- Mutants, run against the real database and reverted (not committed): M1, attempts run on a 30 s context instead of the caller's: the test fails ("Spend ran on after its context deadline"). M2, three detached attempts after the return: the test fails ("2 attempts were started after Spend returned (10 at the return, 12 once the statements ended), want at most the one already sent").
- Risks, and how they are contained: test-only change; the production retry code is untouched. If the test fails again on `main`, the cause is not an in-flight statement.
- Tests added (INV-07, AGENT-12): `endTheLoginsBackends` helper used by the existing test.
- Regression cases added: the existing test, made deterministic and tighter.
- Debt entries: none.
- Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: if the test fails again on `main` after this merges, read the log for the three counts (at the return, after the statements ended, later) and reopen.
- Do not touch: `services/core/execution/ledger/retry.go` (no attempt starts once the context is done).
