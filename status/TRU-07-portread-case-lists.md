# Session 2026-11-20 · TRU-07 (portread: the case list and the complaint_ref list)

State: round 2 (A473) green locally; first round: green locally (gofmt, go vet, go test -race of the package, lint); 38 hand mutants of `cases.go`, 5 survived the first tests and each got a test, all killed. DB tests: none in this package. EL pushes.

## Session summary
- What changed (PR links): see the PR of branch `feat/TRU-07-portread-case-lists`. `services/core/evidence/portread`: `cases.go` (`CaseSources`, `Reader.ReadCases`), new `Reader` fields `Cases`, `CaseRecords`, `CaseSources`, `MaxPayloadBytes`; `cases_test.go`; README.
- Why: ruling A461 (interim, relayed, not yet committed; recorded in docs#212 and docs#213): the stand-in's case list is mock-case's `CasePage`, `complaint_ref` is its own list with its own completeness marker; the portread slice builds both. A438: a list is "none" only when marked complete, otherwise UNKNOWN (#818 reads them through `ListRows` with keys `items` and `complete`).
- Risks, and how they are contained: see the PR body, "Safer readings taken".
- Tests added (REQ, INV, US or EC IDs): `cases_test.go` (REQ-CTX-001, REQ-CTX-007; INV-12, INV-19, INV-21; AGENT-09).
- Regression cases added: none (a feature).
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the composition root sets `Reader.Cases`, `CaseRecords`, `CaseSources` (from the pack's evidence descriptors) and `MaxPayloadBytes` (from a `NOEMA_*_BYTES` setting with no default), then calls `ReadCases`.
- Open questions (owner): none (A473 settled it).
- Safer readings taken (for the PR body, A473, interim, relayed, recorded in docs#216, not yet merged): (1) The complaint item for an unusable or unavailable case list is MALFORMED-class with no provenance, so UNKNOWN, no payload, no bytes (the item says the list could not be read; the read class is not the source's, since no complaint call was made). A case list that failed with a timeout or a 5xx gives an UNKNOWN complaint item, not an UNAVAILABLE one, because the complaint source was never asked. (2) An incomplete case list from an answering source also counts as unusable (its complaints cannot be known): UNKNOWN complaint item. (3) An empty list marked complete from an answering source stays "no cases": one item, no complaint item. (4) A refused Record of the UNKNOWN complaint item returns the case item with the error (EC-E16).
- Round 2: 8 hand mutants of the A473 change, 7 killed, 1 equivalent (`len(ids)==0` vs `true` where the caller ignores the flag for a non-empty list).
- Files to read first: `evidence/portread/cases.go`, `trust/reconcile/mapping/lists.go`.
- Do not touch: `evidence/payloadread`, `evidence/injection`.
