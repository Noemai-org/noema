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
