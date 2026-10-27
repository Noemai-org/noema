# workflows

**What it does.** Temporal workflows and activities. Workflow code does no I/O and reads no clock; every effect is an activity (INV-30). Postgres is authoritative; a workflow's result is a convenience.

**REQs.** REQ-VER-004 (Temporal for durable work), REQ-EXE-006 (a write activity runs once).

**Built.**
- `retry.go`: the table of activities and how each retries (write: one attempt; read: bounded attempts with backoff), and `options.go`, which turns it into Temporal options. A scanner test holds every `MaximumAttempts` in the core to the rule.
- `escalate.go` and `activities.go`: the workflow `EscalateCase` for the graph `fin.graph.escalate` (create the case, read it back by the receipt's idempotency key, ask Trust for a route grant bound to the version token read back, route). It stops at the first write that is not acknowledged; nothing further is written and Temporal never retries a write. Its end state is `AWAITING_VERIFICATION`, never "succeeded": that word belongs to Outcome.
- The activities call three ports: `GrantExecutor` (implemented over the controller by `execution/bridge.GrantExecutor`), `CaseReader` (over the case adapter, `execution/bridge.CaseReadBack`) and `RouteGrantIssuer` (Trust: not built; tests supply a stand-in that signs with a synthetic key).

**Not built.** Outcome watches, confirmation expiry, recovery and callback delivery workflows (OUT-03); the worker process and its registration in `cmd/core`; running against the Temporal server of the Compose stack. The tests use the Temporal SDK's in-process test environment.

**Tests.** `go test ./workflows/...` from `services/core/`: order of calls, stop on every kind of non-acknowledged write, no Temporal retry of a write, bounded read retries, refusal of incomplete input, options equal to the declared policy, and a static check that the workflow file imports nothing that does I/O.
