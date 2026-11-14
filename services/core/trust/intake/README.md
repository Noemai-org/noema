# trust/intake

**What it does.** The Trust intake assembles the steps T1 to T15 of PERD 23.3 into one service, `Service.Evaluate(ctx, Request) (Result, error)`. The order, the stop rules and the fail-closed behaviour are `trust/decide`'s (`EvaluateBound`); the intake supplies the step functions and reaches everything outside Trust through the interfaces of `ports.go`, so Trust imports no execution, evidence or understanding package (INV-01, SoT 51). The composition root supplies the adapters; EL's `Trust.Decide` adapter calls this service.

**REQs.** REQ-TRU-002 (fixed order, refuse before read), REQ-TRU-003 (T1 to T6 here), REQ-NFR-002 (any failure is UNKNOWN, never ALLOW), REQ-TRU-011 (a typed request, not a conversation).

**Status (14 Nov 2026).** T1 to T6 are built: the admitted intent (tenant, version, latest, verified), the session (tenant, principal, the intent's principal is the session's), the kill switch for a request that leads to a write, the requested capability (registered, not prohibited), scope, and the role (`Session.Role`, A205 Q5). Steps T7 to T15 are **not built** and return `ErrNotBuilt`, so the evaluation ends in UNKNOWN INTERNAL_ERROR at T7: **this package cannot allow anything yet**. Next: T10 to T12, T7 to T9, T13 to T15 and the Decision Record (the G1 card of noema#436).

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
