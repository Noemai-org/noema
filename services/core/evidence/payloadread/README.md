# evidence/payloadread

The verified payload read of the Reconciler (ENV-04 slice 2a; noema#596; REQ-CTX-001, REQ-AUD-001; INV-12, INV-19, INV-21; AGENT-13). Updated 16 Nov 2026. All data is SYNTHETIC.

`Reader{Payloads}.Payload(ctx, tenantID, item)` returns the object of an evidence item's stored payload, **only when its canonical hash is the item's `PayloadHash`**. The Trust login's reader (`cmd/core`, `trustPayloads`, ruling A228) gives PostgreSQL's `jsonb` text; the hash covers the RFC 8785 form of the payload (`layer.Builder.Record`); this package decodes with exact numbers (`json.Number`, nothing rounded), makes the object canonical again with `crypto.CanonicalJSON`, hashes it and compares.

| Step | Rule | Refusal |
|---|---|---|
| Context | the caller's context has a deadline | `ErrNoDeadline` |
| Call | a reader, a tenant, an evidence ID, a payload reference, and a hash that is `sha256:` and 64 lower-case hex; nothing is read otherwise | `ErrUnusable` |
| Read | the tenant is the caller's, never the item's or the payload's (INV-12); any failure (not found, no pool, driver, deadline) is "this item cannot be classified", never a pass | `ErrUnavailable` (wraps the cause) |
| Decode | exactly one JSON object and nothing after it | `ErrNotObject` |
| Canonical | no fraction, no exponent, no integer over 2^53-1 (`CanonicalJSON` refuses them); the cause is dropped so no payload text rides in an error | `ErrNotCanonical` |
| Hash | `"sha256:" + hex(SHA-256(canonical form))` equals the item's hash | `ErrHashMismatch` |

On any error the object is nil. No error carries a payload, a hash or a reference (AGENT-13).

**Not here.** The payload size bound (a configuration value of the reader, noema#606), the real read (`cmd/core`), and the `Classify` that builds `reconcile.Evidence` from these objects and the mapping (slice 2b, after noema#589 and noema#613 merge and the Architect answers `Required` on noema#579).

**Tests.** `go test ./evidence/payloadread/` from `services/core/`. They use a fake of the reader port and need no database; 300 generated payloads are checked against the builder's own hash, in canonical and pretty-printed form. 15 hand mutants on `payloadread.go` were killed (listed in the PR).
