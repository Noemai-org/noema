# record/admission

**What it does.** The Record-backed read behind the orchestrator's `Admissions` port (STM-01; A161, A169). `Reader.Lookup(ctx, tenantID, eventID)` finds one event in the tenant's chain through the tenant's own `record_read` pool (the database binds the tenant, DEBT-04), reads in one read-only REPEATABLE READ transaction, verifies the chain (`chain.VerifyRows`) before it trusts a row, and returns `Found`: the event type, the tenant, the principal (the actor after its kind), the correlation and the payload's `request_id`. It decides nothing: the orchestrator compares those facts with the session and the turn. Another tenant's event, and an ID that does not exist, are both `ErrNotFound`.

**REQs.** REQ-AUD-001, REQ-SEC-004, REQ-STM-001. SoT: INV-12, INV-30.

**Not here.** The adapter from `Found` to `orchestrator.Admission` (it lands with the PR that follows noema#465, which defines that type); a keyed read (the chain is read whole per lookup, the same debt as the input gateway's index: a keyed Record read function is a migration, owner C2/EL).

**Tests.** `go test ./record/admission/` from `services/core/`. The tests that append signed Record events need the signing extension (the CI database has it).
