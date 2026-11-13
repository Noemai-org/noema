# understanding/intentstore

Date: 2026-11-12 · Owner: Core Engineer 2 (C2) · Status: built, not yet merged (PR noema#471)

The admitted-intent store (Architect ruling A151). It keeps every Intent Object that the second schema gate (`understanding.Gate`, UND-04) admitted, one row per version, with the hash the gate computed, and appends the Record event `INTENT_ADMITTED` first, in the same transaction. Trust reads an intent by reference; Execution reads the hash of the latest admitted intent of an investigation at E3 (noema#447, noema#448).

## What is built

| Piece | What it does |
|---|---|
| `migrations/0026_admitted_intent.sql` | Table `admitted_intent` (key `tenant_id, intent_id, version`), insert-only (`reject_mutation` on update, delete and truncate), row-level security with `FORCE`, a tenant guard on insert, grants: INSERT and SELECT to `orchestrator_rw`, SELECT to `trust_rw` and `execution_rw`, nothing to anyone else. Also `GRANT EXECUTE ON FUNCTION decision_login_tenant() TO orchestrator_rw` (its policies need it). |
| `Store.Save(ctx, actor, Admitted) (Ref, error)` | One transaction: `INTENT_ADMITTED` (as `core-workflows`, the orchestrator login's emitter), then the row. Before anything is appended, `Save` binds what it writes to itself: the document must be the canonical (RFC 8785) form, `intent_hash` must be `sha256:` of exactly those bytes, and the tenant, intent id, version, supersedes, correlation, node and `status: VALIDATED` of the row must be the document's own; otherwise `ErrNotBound`, no event, no row. If either write fails neither stands (REQ-AUD-001, EC-E16). Payload keys: `intent_node`, `intent_id`, `version`, `intent_hash`; never the text of an input. |
| `Reader.Get(intent_id, version)` and `Reader.Latest(correlation_id)` | Read as the reader's own tenant login. Another tenant's row is `ErrNotFound`, not an error that confirms it exists. |

`Admitted` is plain values (it does not import `understanding`); the caller copies the fields from `understanding.AdmittedIntent`.

## What the database refuses (each tested)

A document that disagrees with its columns (intent id, tenant, correlation, version, `supersedes`, node, status other than `VALIDATED`, a fractional version); a version 1 that supersedes, or a version n that does not name n-1 of the same intent; a version that skips its predecessor or is in another correlation; a second intent in a correlation; a hash or id off shape; a row by another tenant's login or by any power other than the orchestrator; any update, delete or truncate by any role.

## Constraints and open points

- `intent_hash` is stored as given by the gate. The form of the hash (JCS refuses non-integer numbers, intents carry fractional confidences) is the open question on noema#454; the gate fails closed with `HASH_UNAVAILABLE` until it is answered. This store does not recompute it.
- The tenant is bound twice (a `BEFORE INSERT` guard and the RLS `WITH CHECK`). Each alone is hidden by the other in a test; removing both is caught.
- The number is 0026 on C1's migration table (noema#183, ruling A167). 0022 stays reserved for the evidence-set binding of A119 (EVD-02) if noema#410 returns.
- No wiring in `cmd/core`: the composition root is EL's.

## Run its tests

```bash
cd services/core && go test -race ./understanding/intentstore/
```

They start a throwaway Postgres through `internal/testpg`.

## Implements

REQ-INT-001, REQ-SEC-004, REQ-AUD-001; INV-11, INV-12, INV-17, EC-E16; rulings A151, A136.
