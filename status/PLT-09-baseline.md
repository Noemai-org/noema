# PLT-09 · baseline fixes in services/core

## State
- In review (draft PR noema#786). golangci-lint 2.5.0 with `errcheck`, `contextcheck`, `forbidigo ^panic$`, tests excluded, caps off: `services/core` went from 32 findings to 1 (`edge/session/http.go:74`, contextcheck, left as is: it flags a `context.WithValue(r.Context(), ...)` that does inherit; not fixed by a rewrite). The lint config used is a local one, not committed (the gate PR is separate, OD-P-99).

# Session 2026-11-19 · PLT-09-baseline

## Session summary
- What changed (PR links): noema#786. Rollback contexts in 5 recorders use `context.WithoutCancel(ctx)` (same timeout, same detachment, now inherits values); `_ = Close()` on read-only handles; `_ = out.write` where the stream ends anyway; `_, _ = fmt.Fprint*` on CLI writers; `platform/crypto` small-order table loads through `loadSmallOrderY` and a failed load refuses every key (was a panic at package init, unreachable with the shipped constants); `testpg.DecisionDocJSON` returns an empty (jsonb-invalid) document instead of panicking.
- Why: PLT-09 baseline, REQ-TST-010.
- Risks, and how they are contained: P0 paths (Record recorders) change only the context the rollback runs on; DB tests run in CI only (no pgsodium locally).
- Tests added (REQ, INV, US or EC IDs): REQ-TST-010, INV-11: `platform/crypto/ed25519keys_load_test.go` (3 tests, seen failing against the stub).
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: `benchmark/hostility_proxy/internal/proxy/requestlog.go:44` panic and the http.go contextcheck remain; the gate PR needs them fixed or its config decided.
- Do not touch: the lint config, `.github`.
