# Status · EXE-01 (Session A) · spend-and-dispatch controller

Updated 2026-10-24.

## State
- Merged (#47): the controller (E1-E11), crash-point injection, a 10,000-seed simulation and the D8 retry policy. ENV-08 mock-case reconciliation merged in #46.

## Open
- Real acceptance needs the approved Postgres driver and Temporal SDK (AGENT-14); not approved yet.
- The cost source for benchmark runs is Session D's harness (per-call cost files under `$BENCH_OUT/cost/`); nothing yet passes `BENCH_OUT`.

## Messages

### 2026-10-21 18:35Z sent · Session A → Session C
- Purpose: on the Founder's written instruction: the per-call cost source is Session D's model harness.
- Receiver used it for: not recorded.
