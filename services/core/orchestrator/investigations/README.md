# orchestrator/investigations

**What it does.** The store of the investigation projection of PERD 28.2 (STM-01 slice 2). Migration `0025_investigation.sql` holds one row per investigation, unique per `(tenant, input_received_ref)`. `Store.Start` makes the investigation of a recorded input or finds the one an earlier call made (idempotent, noema#477; `created` is true for exactly one caller). `Store.AppendChange` is the `orchestrator.Recorder`: in one transaction it locks the row, refuses a change made against another state or version (`ErrStaleVersion`), appends `INVESTIGATION_STATE_CHANGED` as `core-workflows`, and only then moves the row with a compare-and-swap on the version the caller read (SR's note 2 on noema#264). If the append fails, neither the event nor the move remains (the Record comes first, EC-E16).

**REQs.** REQ-STM-001, REQ-AUD-001, REQ-SEC-004. SoT: INV-12, INV-30.

**What the database enforces** (migration 0025): the state is one of the machine's (a test compares the lists); a new row is RECEIVED at version 0; a row moves by exactly one version; only the state, the version and the time ever change (column privileges); no delete or truncate; the tenant is the login's (row-level security with FORCE); only the `orchestrator_rw` role touches the table. **What it does not:** the legality of an arrow stays in the Go machine (`transitions.yaml`); the database has no copy of the table.

**Not here (stated).** (1) `Runner.RunRead` does not call `Start` yet: the runner takes an `Investigations` port in the PR that follows noema#465 (the recorded-input check), which changes the same files. (2) Resume of a stranded run: the table has no failure state, and re-running `Trust.Decide` would record a second decision, so resume waits for that to be settled. (3) The Record-backed `Admissions` read of the INPUT_RECEIVED event.

**Tests.** `go test ./orchestrator/investigations/` from `services/core/` (needs PostgreSQL). `table_test.go` is SQL only. `store_test.go` appends signed Record events and so needs the signing extension (the CI database has it).
