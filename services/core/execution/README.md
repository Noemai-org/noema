# execution

**What it does.** Planner, capability registry, execution controller, recovery.

**REQs.** REQ-EXE-002, REQ-EXE-003, REQ-CAP-002.

**Status.** `controller/` is built (T6.5, DEC-2026-10-20): the E1 to E11 protocol of PERD §27.1 over ports, E10 classification, the recovery sweeper, crash-point injection at 14 boundaries, and a seeded simulation of 10,000 interleavings. The Postgres ledger, the case adapter and the real mock-case acceptance are not built (they need the dependency decision of AGENT-14 and noema#46). `planner/`, `registry/` and `recovery/` stay empty boundary packages.

**Tests.** `go test -race ./execution/... ./workflows/ ./internal/vectors/` from `services/core/`. Replay one simulation seed with `NOEMA_DST_SEED=<seed> go test -run TestDeterministicSimulation ./execution/controller/`.

## Invariant-to-test map (PERD §45.2; TST-01, completed by TST-03)

| Invariant | Statement | Test | State |
|---|---|---|---|
| INV-03 | No dispatch without a valid, unexpired, unused, matching grant | `controller/inv_property_test.go` `TestINV03…` (fourteen defects × every network fault × 1 to 4 presentations); also `TestDeterministicSimulation` (10,000 seeds) | proved |
| INV-05 | Prohibited and unregistered IDs are never callable | EXE-01 (registry, static scan, property test) | not yet built |
| INV-07 | Timeout, 5xx or silence never maps to success or failure | `TestINV07…` (every fault; every crash point then the sweeper); `classify_test.go` | proved |
| INV-08 | Key committed before dispatch | `TestINV08…` (every crash point × every fault) | proved |
| INV-18 | Plans contain only registered, non-prohibited IDs | PLN-01 (planner, plan hash, property test) | not yet built |

The properties are written with `pgregory.net/rapid` (test-only, DEC-2026-11-03). Run more cases with `-rapid.checks=1000`; a failure prints its seed. Each was mutation-checked: skipping the signature check, skipping the expiry check, sending a different idempotency key, and reading a 5xx as a terminal failure each make the matching property fail. "Matching" in INV-03 means plan, capability, parameters and tenant; the target is bound by the grant's signature, because the plan has no target field.
