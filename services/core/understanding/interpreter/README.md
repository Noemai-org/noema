# understanding/interpreter

**What it does.** The core's step between the model service's proposal and the admitted intent (UND-04, core half; rulings A166, A168, A177). `Interpreter.Interpret(ctx, Input)` takes the session, the actor (`<kind>:<id>` of the session's principal), the correlation id and the compile request, then:

1. refuses an incomplete input (`ErrInput`) before it calls anything;
2. asks the model service to compile (`understanding.Client`; a failure is a `*CompileFailure` in one of the client's four classes, or `ErrCompile`);
3. runs the core's gate (`understanding.Gate.Admit`);
4. **a refusal at the gate** is appended as `INTENT_REJECTED_AT_GATE` with its reason codes (the key `reason_codes`, as `record/reconstruct/project.go` reads it) and returned as the gate's `*understanding.Rejection`; if the Record refuses the append the caller gets `ErrRecord`, never "refused" (the Record comes first, EC-E16). A session the gate finds malformed is the caller's bug: `ErrInput`, nothing recorded;
5. **a real identity override** (the model service returned another tenant, principal or session) is appended as `SECURITY_EVENT` `PRINCIPAL_OVERRIDE_ATTEMPT` with the field NAMES, before the save; the intent saved is the session's. A normal compile records nothing of its own (A166, item 3);
6. **saves** the admitted intent through `intentstore.Store.Save`, which appends the signed `INTENT_ADMITTED` event and stores the row in one transaction (A177: that event is the signature; the core computes `intent_hash` over the canonical JSON). A failed save is `ErrSave`.

No message of any error prints the input, the proposal, an admitted document or a cause: the cause stays reachable with `errors.Is` and `errors.As` and is never part of `Error()`.

**REQs.** REQ-INT-001, REQ-INT-002, REQ-INP-002, REQ-AUD-001. SoT INV-02, INV-12, INV-17, EC-E16.

| File | What |
|---|---|
| `interpreter.go` | `New(Config)`, `Interpret`, the four ports (`Compiler`, `Admitter`, `Recorder`, `Saver`), the error kinds |
| `record.go` | `RecordRecorder`: the two events as the orchestrator's emitter (`core-workflows`), one transaction per event, through the tenant's own orchestrator login (the database binds the tenant and the emitter) |

**For EL (composition root).** Build `Config` with `understanding.Client` (the mTLS `http.Client`), `understanding.Gate` (`NewGate` over `NewPackView` of the mounted pack), `RecordRecorder{Pool: <the tenant's orchestrator login pool>, Appender, Timeout}` and `intentstore.Store` on the same login. `Interpret` is called once per compile; it is not idempotent per input (a second save of the same intent version is refused by the store's unique key), so a caller that retries a turn decides what a retry means.

**Not here.** The model service's compile route and compiler (`compiler/` there is empty); `INTENT_PROPOSED` (the compile answer carries no provider or model version yet); `cmd/core` wiring.

**Tests.** `go test ./understanding/interpreter/` from `services/core/`. `interpreter_test.go` needs no database (spies for the model service, the Record and the store; the gate is the real one over the real pack `packs/fintech_v1.json`). `record_test.go` runs against PostgreSQL with pgsodium (per-emitter signatures, REC-08): CI's `core` job.
