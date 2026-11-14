# trust/intake

**What it does.** The Trust intake assembles the steps T1 to T15 of PERD 23.3 into one service, `Service.Evaluate(ctx, Request) (Result, error)`. The order, the stop rules and the fail-closed behaviour are `trust/decide`'s (`EvaluateBound`); the intake supplies the step functions and reaches everything outside Trust through the interfaces of `ports.go`, so Trust imports no execution, evidence or understanding package (INV-01, SoT 51). The composition root supplies the adapters; EL's `Trust.Decide` adapter calls this service.

**REQs.** REQ-TRU-002 (fixed order, refuse before read), REQ-TRU-003 (T1 to T6 and T8 here), REQ-AMB-001 to REQ-AMB-004 (T7), REQ-CTX-003 (T9), REQ-NFR-002 (any failure is UNKNOWN, never ALLOW), REQ-TRU-011 (a typed request, not a conversation).

**Status (14 Nov 2026).** T1 to T9 are built. T1 to T6: the admitted intent (tenant, version, latest, verified), the session (tenant, principal, the intent's principal is the session's), the kill switch for a request that leads to a write, the requested capability (registered, not prohibited), scope, and the role (`Session.Role`, A205 Q5). T7 to T9 (`gates_t7_t9.go`, ports in `ports_t7_t9.go`) are the only steps that read about a target, each through its own port and nothing else:

- **T7 target resolution** (`Candidates`): a resolved intent passes and reads nothing. An unresolved one reads the candidates scoped to the session's principal and the pack's disambiguation data, keeps those at or above the floor, and builds the question with `decide.BuildClarification` (CLARIFY `AMBIGUOUS_COUNTERPARTY`), or ESCALATE `AMBIGUOUS_COUNTERPARTY` when none reaches the floor, more reach it than the pack's maximum, or no question can be built (one candidate, labels that do not tell the options apart). Scores are integers in the pack's unit: Trust decides on no float and holds no number.
- **T8 business ownership** (`Ownership`): a target the principal does not own and a target that does not exist are the same REJECT `NOT_FOUND_OR_NOT_AUTHORIZED`, with the same one port call and the same trace.
- **T9 evidence** (`Evidence`, with `Request.EvidenceSnapshotID` and `EvidenceUnavailable`, the orchestrator's `EvidenceRef`): an unavailable required source is ESCALATE `INSUFFICIENT_EVIDENCE`; an item past its effective limit that the Evidence Layer could not refresh is ESCALATE `STALE_EVIDENCE`; otherwise the snapshot and the freshness the port reported are recorded once in the evaluation's `BasisBuilder` and the step continues.

Steps T10 to T15 are **not built** and return `ErrNotBuilt`, so the evaluation ends in UNKNOWN INTERNAL_ERROR at T10: **this package cannot allow anything yet**. Not built here: the candidate-set ID, version and expiry of a clarification (they belong to the Record and the answer binding), and the adapters behind the three ports.

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

**Tests.** `go test -race ./trust/intake/` from `services/core/`. `gates_test.go` and `target_ports_test.go` run whole evaluations through spies; `t7_t9_test.go` (package `intake`) builds the evaluation after T6 and runs one of T7 to T9 at a time.
