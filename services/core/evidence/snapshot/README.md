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

**Database objects of migration 0020** (EVD-03 slice 2; the number is the Repo Steward's, on the ledger at noema#183; this table lives here because no pull request edits `migrations/README.md`, A145).

| Object | Rule |
|---|---|
| `evidence_set_hash_of(ids, hashes)` | the golden-vector construction in SQL (sorted by the UTF-8 bytes of the id, `LP(id) LP(hash)`, SHA-256); errors on an empty or repeated id, a bad hash, arrays of unequal length; `EXECUTE` for `trust_rw` alone |
| `context_snapshot` | insert-only (triggers); at least one evidence id; the empty-set hash is never stored; written by the tenant's own Trust login (`trust_rw` holds SELECT and INSERT), forced row-level security on `tenant_id = evidence_login_tenant()` (A6, A136) |
| `context_snapshot_guard()` | `SECURITY DEFINER` insert trigger: the login is a Trust login of `NEW.tenant_id` (`session_user`), the ids are non-NULL, sorted and unique, every id names an evidence item of that tenant, and the stored hash equals the hash the database computes from those items' payload hashes |
| `evidence_item_definer_read` policy | `FOR SELECT TO CURRENT_USER USING (tenant_id = evidence_login_tenant())`: the guard is `SECURITY DEFINER` and `evidence_item` forces row-level security, which binds the owner; without a policy for the owner a non-superuser migration login (Compose) sees no evidence and refuses every snapshot. `TestEverySecurityDefinerFunctionThatReadsAForcedTableHasAPolicyForItsOwner` pins the class and `test_init.sh` seals one snapshot as the non-superuser migrator |
| `evidence_verify` (created in `init.sh`, not here) | a LOGIN role in no group; `USAGE` on the schema, `SELECT` on `context_snapshot` and `evidence_item` only, and one `FOR SELECT ... USING (true)` policy on each table; no payload table, no Record, no function |

**Tests.** `go test ./evidence/snapshot/` from `services/core/`. `db_test.go` pins the table, the guard and the exact grants of `evidence_verify`; `seal_test.go` the sealer and the recompute. Written first; the mutation checks are in `status/EVD-03.md`.
