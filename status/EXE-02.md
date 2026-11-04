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
