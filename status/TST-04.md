# Status · TST-04 · Test-suite audit

Updated 2026-11-07. Owner: EL. Done when: audit note committed with any exceptions fixed (REQ-TST-001, REQ-TST-004, REQ-TST-006).

## State
- Audit note: `docs/audits/TST-04-test-suite-audit.md`. In review.
- SR decided the resolver fake (noema#293): recorded exception "resolver seam in platform/egress only", with a system-resolver test due in SEC-05 slice 2. One open: `BENCH_OUT`, the Architect's.
- Not Done: two exceptions (the `fakeResolver` in `egress_test.go`; `BENCH_OUT` in the model service) are listed, not fixed, because fixing them is outside this task's files and needs owner decisions. The task's "any exceptions fixed" clause is therefore not met until those are decided.

# Session 2026-11-07 · TST-04

## Session summary
- What changed (PR links): the audit note and this file.
- Why: Block E item 7.
- Risks, and how they are contained: documentation only.
- Tests added: none. Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the owners of SEC-05 and the model service decide the two exceptions; then fix or record them.
- Open questions (owner): are the two exceptions accepted? (SEC-05 owner; Architect for BENCH_OUT)
- Files to read first: `docs/audits/TST-04-test-suite-audit.md`.
- Do not touch: `services/core/platform/egress`, `services/model/noema_model` from this task.

# Session 2026-11-08 · TST-04

## Session summary
- What changed: both exceptions decided (SR: resolver seam; Architect, noema#293: `BENCH_OUT` accepted for the POC on two conditions). Tests added in `services/model/tests/test_cost_files.py`: decisions identical with it unset; cost files only under `$BENCH_OUT/cost` and never through a link; the bench compose override mounts no held-out custody (INV-22). The custody test failed first and found that the writer followed a symbolic link; fixed in `platform/telemetry/__init__.py`.
- Tests: the model service suite passes (686) and `mypy --strict` is clean on the changed module.
- Done-when: the audit note is committed and the exceptions are decided and recorded. One follow-up is owed elsewhere: SEC-05 slice 2's system-resolver test.

# Session 2026-11-08 (later) · TST-04

## Session summary
- What changed: the Architect's ruling A55 (SR's condition governs): `BENCH_OUT` no longer lives in runtime code. The cost-file writer moved from `services/model/noema_model/platform/telemetry` to `benchmark/model_cost/cost_files.py` (stdlib only); `benchmark/model_cost/model_app.py` is the bench entry point; `deploy/compose/bench-model.yml` mounts it read-only and starts it; `build_telemetry()` takes no environment; `bench-model-up.yml` updated. Tests moved or rewritten, failing first.
- Tests: `benchmark/model_cost/test_cost_files.py` (7); `services/model/tests/test_main_telemetry.py` (service ignores BENCH_OUT, runtime holds no writer, entry point writes the record, compose mounts only bench output and the read-only code). Model service suite 661 passed; `mypy --strict` clean on the changed modules.
- Risks: P0 paths untouched. `deploy/compose/bench-model.yml` is bench-only; a compose change may need a Founder's approval (as for noema#268). The container start is not run here (no Docker).
- Not done: DEBT-26 for the remaining exception is on docs#140 (the Steward's).
