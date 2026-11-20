# Session 2026-11-20 · TRU-07 (portread: the case list and the complaint_ref list)

State: RED commit (tests and a stub). The implementation follows in a local commit that EL pushes.

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
- Open questions (owner): whether a complaint list with no case to read it for needs an explicit UNKNOWN item (EL).
- Files to read first: `evidence/portread/cases.go`, `trust/reconcile/mapping/lists.go`.
- Do not touch: `evidence/payloadread`, `evidence/injection`.
