# evidence

**What it does.** The read-only Evidence Layer: items, freshness, candidates API.

**REQs.** REQ-CTX-001, REQ-CTX-002, REQ-CTX-003.

**Status.** EVD-01 slice 1 (the task card is docs#109, a draft): `layer.AssignStatus`, the pure and total FactStatus function of REQ-CTX-002, with tests (a table of the PERD §21.1 rows it can decide, absence never KNOWN, whatever a source declares (PERD §21.1's AUTHORITATIVE_NEGATIVE exception is not built: evidence-item.v1 forbids it and evidence-source.v1 has no field to declare it), totality and determinism over 50,000 seeded generated reads, and a Go fuzz target). It returns KNOWN, STALE, UNAVAILABLE, UNKNOWN or UNAUTHORIZED; INFERRED, AMBIGUOUS and CONTRADICTORY need more than one item and a reconciliation rule (RES tasks). Not built yet: the `evidence_item` and `evidence_payload` tables and their immutability trigger, the item builder, refresh, snapshots and the candidates API (EVD-01 slice 2 onward, EVD-02 to EVD-06). The import rules and prohibitions are in `doc.go`.

**Decisions (the card's D1 to D5, reversible).** A value from a corroborating-only source, from a source with authority NONE, or read from the future is UNKNOWN; the rules run most restrictive first; the function lives in `layer`.

**Tests.** `go test ./evidence/...` from `services/core/`; fuzz with `go test -fuzz FuzzAssignStatus ./evidence/layer/`.

**EVD-03 (sealed context snapshots, REQ-CTX-004, INV-17).** `snapshot/`: `SetHash` (the evidence_set_hash against the golden vectors), `Sealer.Seal` (sorts and hashes the set, refuses the empty set, appends `CONTEXT_SNAPSHOT_SEALED` and stores the row in the caller's transaction), and `Recompute` (the nightly check: every stored hash recomputed from the stored items, run as the read-only `evidence_verify` login; its output carries tenant, snapshot and digests only). The table, its guard trigger and the login's grants are migration 0020; see `snapshot/README.md`.
