# Session 2026-11-20 · TRU-07 (the evidence mapping's list entities, fin.map.v1@2)

## Session summary
- What changed (PR links): noema#818. `services/core/trust/reconcile/mapping`: `lists.go` (new: `rows_from`, the `complete` marker, `ListRows`, `WithScope`, `ErrIncomplete`, `ErrListShape`, `ErrNoSuchList`), `mapping.go` (`Parse` takes options; `Rows` skips list entities). Pack data: `packs/fintech/evidence_mapping/mapping.v1.json` is version 2 with the `case` and `complaint_ref` list rows; `packs/fintech_v1.json` declares `complaint_ref.effective_at` (Timestamp, the clock anchor); `packs/fintech/manifest.v1.json` names `fin.map.v1@2` and carries both new SHA-256 pins.
- Why: ruling A438 (interim): a `rows_from` construct for list entities; "no case" only from a list marked complete, otherwise UNKNOWN, never none; the stand-in's list carries the marker; bump to `fin.map.v1@2` with the manifest; the clock anchor is `complaint_ref.effective_at`.
- Risks, and how they are contained: (1) every caller that parses the real mapping file now passes `mapping.WithScope(...)` (the pack's evidence descriptors) and lists `fin.src.case_system` among the sources; the test callers in `evidence/reconciler` and `orchestrator/portadapters/trust/reconciler` were updated, and noema#773 (wire_understanding, unmerged) must do the same when it merges; (2) the Reconciler is unchanged: `Rows` does not return list entities, because `reconcile.Reconcile` errors on an entity outside its rule set; (3) the lint rule `money-float-area` forbids `float64` in `trust`, so the code and tests read integers as `json.Number` (the payload reader's own shape) and accept no float.
- Tests added (REQ, INV, US or EC IDs): `lists_test.go` (REQ-RES-001, REQ-CTX-001; INV-19, INV-21), 13 tests; the existing mapping, reconciler and packcheck tests follow the id bump (`fin.map.v1@2`) and pass the scope to every `Parse` call so each refusal stays for its own reason.
- Regression cases added: none (a feature).
- Debt entries: none.
- Conflicts raised: none.

## Rules the tests pin
- `{items: [], complete: true}` is no case. A list whose marker is false, missing, a string, a number or null is `ErrIncomplete`, with or without rows. A payload that is not the named list of objects is `ErrListShape`. No rows come back on an error.
- A field an element does not give, or gives in another shape (an unlisted enum value, a fraction, an out-of-range integer, an instant with no zone, a control character), is left UNSET, never defaulted (INV-19).
- Twelve kinds of unsound list row are refused at load; an entity may be in one scope only; a plain row may not carry `complete`.
- The clock anchor is the case system's own `clock_started_at` (not `logged_at`).

## Hand mutants
21 hand mutants of `lists.go` and `mapping.go` (marker handling, list shape, scope rule, enum map, timestamp zone and value, integer range, control characters, empty strings, field loop, `Rows` skipping, plain-row marker); 2 survived the first tests (the both-scopes test refused for another reason; a marker on a plain row was ignored) and each got a test; all killed.

## Not done, and who does it
- The producer of the case payload (the `portread` case slice, EL) and the FactSource that calls `ListRows` (C1, port in `trust/intakeadapters/facts.go`). The payload keys follow mock-case's page shape (`items`, `complete`).
- `complaint_ref.subject` and `.epoch` are not mapped: the complaint payload carries neither.

## Pre-work for the next agent
- Exact next step: the FactSource implementation reads the sealed case item, calls `ListRows(source, "case", payload)` and treats `ErrIncomplete` as UNKNOWN.
- Open questions (owner): which payload carries the complaint list (a second item or the same payload) (EL).
- Files to read first: `trust/reconcile/mapping/lists.go`, `lists_test.go`, `packs/fintech/evidence_mapping/mapping.v1.json`.
- Do not touch: `evidence/reconciler` semantics (Reconciler rows are unchanged).
