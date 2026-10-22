# benchmark

Owner task: ENV-09 (hostility proxy and benchmark harness). Status: empty, not built.

This folder will hold the benchmark services that run on `bench_net` only (PERD §65.1). It never mounts anything from `benchmark-heldout` (INV-22, REQ-AGT-004). Held-out material is unreachable from this repository.

## Session C's folders (T4 of DEC-2026-10-20)

Generated layer only. `make bench-test` runs every test and `mypy --strict`; `make bench` is the generated layer end to end (exit 3 is "not run: no access"); `make bench-live` runs the oracle client and every translated fault plan against a real mock-case and PostgreSQL. The hostility proxy and the partner listener belong to session B.

| Folder | What | Task |
|---|---|---|
| `generator/` | `noema-casegen`: the seeded case generator, mutations and the coverage report | BEN-10 |
| `scoring/` | The tiered scorecard and report, a pure function of recorded results | BEN-12 |
| `oracle_client/` | The runner's one reader of ground truth (mock-case's truth endpoint, bench credential) | BEN-03 |
| `runner/` | Drives a suite through the public API, applies fault plans, writes results and a run manifest | BEN-03 |
| `nightly/` | Files one regression issue per failing case, de-duplicated by case ID | TST-02 |
| `bakeoff/` | The decision-model bake-off: case set, metrics, runner, comparison | BEN-21 |
| `evil_provider/` | The adversarial provider, in the bench profile only | SEC-06 |
| `arms/` | The comparison-arm folders (no arm is built here) | BEN-20 |
