# outcome

**What it does.** Decides, from its own reads after the settle window, whether an action achieved what it claimed (REQ-VER-001): the verifier, the predicate binding, the Postgres store of verdicts. Durable watches (OUT-03) and callbacks (REQ-VER-005) are not built.

**REQs.** REQ-VER-001, REQ-VER-002, REQ-VER-003, REQ-VER-004 (the watch row only), REQ-RCV-002; INV-04, INV-07, INV-19, INV-28.

**Status.** OUT-01 (verify by key) is built. Not built: receipt-signature verification (the `ReceiptAuthenticity` port is wired and no implementation holds a key; W1, GOV-28), Outcome's own Ed25519 signer (an interim signer is supplied by the caller), the other pack outcomes (OUT-02), the Temporal watch workflow and sweeper (OUT-03), the inbound event inbox (OUT-04), and the trigger that calls `Verify` from the `outcome.verify` outbox event. Configuration (the read budget, the pending-watch timing) is supplied by the caller; the defaults of PERD §29.2 are OPS-01's.

| Package | What it is |
|---|---|
| `verifier` | `Verify(ctx, Request)`: settle window, read budget, verify by key, fail closed, signing through a port. Reads only through `CaseReader`, which has no write method. |
| `predicate` | `LoadPack`: scope from a pack's descriptors and variables, every success predicate type-checked at load. |
| `store` | `Store` over PostgreSQL as `outcome_rw`: `LoadReceipt`, and `RecordVerdict` (one transaction through `outcome_record_verdict`). |

**How the later checks slot in.** `ReceiptAuthenticity.Check` runs before any read: an invalid signature raises `SECURITY_EVENT SIGNATURE_INVALID` and an `UNKNOWN` verdict. The `Signer` port signs the verdict today and will sign Outcome's Record events (REC-08): only the port's implementation and the database function's signature check change.

**Tests.** From `services/core/`: `go test ./outcome/...` (against a real PostgreSQL through `internal/testpg`) and `go test ./adapters/outcome_e2e/...` (US-06, US-16 and EC-E03 against the real `mock-case` with the committed FaultPlan fixture; the end-to-end tests sit under `adapters/` because the architecture lint allows no other area to import Execution, Outcome and the case adapter together).
