# execution

**What it does.** Planner, capability registry, execution controller, recovery.

**REQs.** REQ-EXE-002, REQ-EXE-003, REQ-CAP-002.

**Status.** `controller/` is built (T6.5, DEC-2026-10-20): the E1 to E11 protocol of PERD §27.1 over ports, E10 classification, the recovery sweeper, crash-point injection at 14 boundaries, and a seeded simulation of 10,000 interleavings. The Postgres ledger, the case adapter and the real mock-case acceptance are not built (they need the dependency decision of AGENT-14 and noema#46). `planner/`, `registry/` and `recovery/` stay empty boundary packages.

**Tests.** `go test -race ./execution/... ./workflows/ ./internal/vectors/` from `services/core/`. Replay one simulation seed with `NOEMA_DST_SEED=<seed> go test -run TestDeterministicSimulation ./execution/controller/`.
