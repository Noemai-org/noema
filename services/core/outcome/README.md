# outcome

**What it does.** Decides, from its own reads after the settle window, whether an action achieved what it claimed (REQ-VER-001): the verifier, the predicate binding, the Postgres store of verdicts. Durable watches (OUT-03) and callbacks (REQ-VER-005) are not built.

**REQs.** REQ-VER-001, REQ-VER-002, REQ-VER-003, REQ-VER-004 (the watch row only), REQ-RCV-002; INV-04, INV-07, INV-19, INV-28.

**Status.** OUT-01 (verify by key) is built. TST-05: `verifier.Config.Boundary` is the crash harness's hook, called once at `PointAfterVerdictStored` ("after verify, before the note", RESIST S9, ruling A40): the verdict and its events are committed and the caller has not yet been handed the result. Production leaves it nil. Not built: the reader of Execution's public keys from the `signing_key` table (W4; `receiptsig.PublicKeys` is the verify-only interface it will implement, and the clause on a key revoked with `compromised_since` waits for it), Outcome's own Ed25519 signer (an interim signer is supplied by the caller), the other pack outcomes (OUT-02), the Temporal watch workflow and sweeper (OUT-03), the inbound event inbox (OUT-04), and the trigger that calls `Verify` from the `outcome.verify` outbox event (it must pass the delivery's id as `Request.VerificationID`). Configuration (the read budget, the pending-watch timing) is supplied by the caller; the defaults of PERD §29.2 are OPS-01's.

| Package | What it is |
|---|---|
| `verifier` | `Verify(ctx, Request)`: settle window, read budget, verify by key, fail closed, signing through a port. A verification has an ID; a redelivery returns the stored verdict and reads and writes nothing. Reads only through `CaseReader`, which has no write method. |
| `receiptsig` | `Checker`: verifies an Execution receipt with `core-execution`'s public key, read through the verify-only `PublicKeys`; INVALID for a receipt that is not genuine, an error when it cannot tell. No private key. |
| `predicate` | `LoadPack`: scope from a pack's descriptors and variables, every success predicate type-checked at load. |
| `store` | `Store` over PostgreSQL as `outcome_rw`: `LoadReceipt`, and `RecordVerdict` (one transaction through `outcome_record_verdict`). |

**The verifier fails closed on the receipt's signature (Founder decision, 4 Nov 2026).** `ReceiptAuthenticity.Check` runs before any read, and only `VALID` goes on: an invalid signature raises `SECURITY_EVENT SIGNATURE_INVALID` and an `UNKNOWN` verdict; a check that cannot run, `NOT_CHECKED` and any unknown status are `UNKNOWN` with no read and no event. `NOT_CHECKED` can never produce `ACHIEVED`. `receiptsig.Checker` is the real implementation: Ed25519 over `noema/receipt/v1\n` and the RFC 8785 receipt without `sig`, key ID `core-execution.ed25519.<n>`, the document equal to the row (DEC-2026-11-01-receipt-and-record-signing D4, D6, D7, §8). **How the later checks slot in.** The `Signer` port signs the verdict today and will sign Outcome's Record events (REC-08): only the port's implementation and the database function's signature check change.

**Tests.** From `services/core/`: `go test ./outcome/...` (against a real PostgreSQL through `internal/testpg`) and `go test ./adapters/outcome_e2e/...` (US-06, US-16 and EC-E03 against the real `mock-case` with the committed FaultPlan fixture; the end-to-end tests sit under `adapters/` because the architecture lint allows no other area to import Execution, Outcome and the case adapter together).
