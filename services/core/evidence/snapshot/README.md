# evidence/snapshot

**What it does.** The sealed context snapshot of REQ-CTX-004: an immutable set of evidence IDs and the hash over their (evidence_id, payload_hash) pairs. Adding evidence creates a new snapshot. Trust never issues a grant over an empty set (INV-17).

**REQs.** REQ-CTX-004, INV-17, INV-11, REQ-SEC-004, REQ-AUD-001. DEC-2026-10-16 C1 (the hash), D9 (the nightly recompute login).

| File | What |
|---|---|
| `hash.go` | `SetHash`: sort by the UTF-8 bytes of evidence_id, `LP(id) LP(hash)`, SHA-256, `sha256:` prefix. Checked against `contracts/fixtures/golden_vectors.json` (SYNTHETIC) |
| `seal.go` | `Sealer.Seal(ctx, tx, Input)`: in the caller's transaction, which must come from the tenant's Trust login, it appends `CONTEXT_SNAPSHOT_SEALED` (written by `core-trust`, payload = the context-snapshot.v1 document) and stores the row. `ErrEmptySet` for no evidence. On any error the caller rolls back and neither the event nor the row stands |
| `recompute.go` | `Recompute(ctx, pool)`: pool must log in as `evidence_verify` (else `ErrWrongLogin`); reads every snapshot and its items in one read-only repeatable-read transaction; `OK`, `MISMATCH` or `MISSING_EVIDENCE` per snapshot. Output: tenant, snapshot, status, stored and recomputed hash; nothing else |

**Two checks, not one.** The database recomputes the hash on every insert (migration 0020), so a runtime login cannot store a hash it did not compute; `Recompute` catches what the database cannot see at insert time, such as a hash or an item changed afterwards by someone with the right to disable triggers. A MISMATCH is a finding for the operator; this package changes nothing and stops no tenant.

**Not built.** The job's schedule and the operator alert (OPS), the event for a failed recompute, and the Trust call site (TRU) that supplies the reconciliation. `Input.Classification` is the reconciliation's classification string as given; the set of classifications is not defined here.

**Tests.** `go test ./evidence/snapshot/` from `services/core/`. `db_test.go` pins the table, the guard and the exact grants of `evidence_verify`; `seal_test.go` the sealer and the recompute. Written first; the mutation checks are in `status/EVD-03.md`.
