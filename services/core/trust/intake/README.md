# trust/intake

**What it does.** The Trust intake assembles the steps T1 to T15 of PERD 23.3 into one service, `Service.Evaluate(ctx, Request) (Result, error)`. The order, the stop rules and the fail-closed behaviour are `trust/decide`'s (`EvaluateBound`); the intake supplies the step functions and reaches everything outside Trust through the interfaces of `ports.go`, so Trust imports no execution, evidence or understanding package (INV-01, SoT 51). The composition root supplies the adapters; EL's `Trust.Decide` adapter calls this service.

**REQs.** REQ-TRU-002 (fixed order, refuse before read), REQ-TRU-003 (T1 to T6), REQ-TRU-008 and REQ-TRU-009 (T13 to T15), REQ-NFR-002 (any failure is UNKNOWN, never ALLOW), REQ-TRU-011 (a typed request, not a conversation).

**Status (14 Nov 2026).** T1 to T6 are built (see `gates.go`). T13 to T15 are built (`gates_t13_t15.go`, ports in `ports_t13_t15.go`): T13 asks the `Confirmations` port, and a confirmation the profile requires and that is not resolved stops with CONFIRM (a given one lets the evaluation go on and the grant follows its event); T14 asks the `Planner` port and validates the plan as plain values, an invalid plan being UNKNOWN `PLAN_INVALID`, and a read having an empty plan with its hash (A205, Q3); T15 builds the ALLOW with all three layers at PASS and the basis of the `BasisBuilder`, calls the `Recorder` port once, and lets the ALLOW stand only if it succeeded (record first, EC-E16). `Result` carries the decision ID and the grant IDs of an ALLOW. The composition implements `Recorder.Record` with `recorder.Writer` inside one transaction it owns. Steps T7 to T12 are **not built** and return `ErrNotBuilt`, so a full evaluation still ends in UNKNOWN INTERNAL_ERROR at T7: **this package cannot allow anything yet**. Known window: if the context ends while `Record` runs, the record may be written while the evaluation returns UNKNOWN; the composition's transaction is the place to close it.

**Example.**

```go
svc := &intake.Service{Ports: ports, Clock: clk, PackVersion: "1.0.0"}
res, err := svc.Evaluate(ctx, intake.Request{
	Session: intake.Session{TenantID: t, PrincipalID: p, Role: role}, // from the authenticated session only
	CorrelationID: corr, IntentRef: "int_abc123@2",
})
// res.Decision.Outcome(), res.Decision.Reasons(), res.Trace (one entry per step reached)
```

A malformed reference is REJECT `INTENT_INVALID` and reads nothing. A missing port, no clock or pack version, or a context without a deadline is an error, never a decision.

**Tests.** `go test -race ./trust/intake/` from `services/core/`.
