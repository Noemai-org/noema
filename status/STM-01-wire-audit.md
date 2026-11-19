# Status · STM-01 · the reconstruction route is audit-only; the audit panel route

## Session 2026-11-19 15:00 WAT · STM-01 (C1, helper session)

## Session summary
- What changed: the reconstruction route `GET /v1/reconstructions/{correlation_id}` serves the audit role only. `rolesOpsAndAudit` became `rolesAudit` in `services/core/cmd/core/compose.go`; `reconstruct.mayRead` accepts the audit role only and the exported `RoleOps` constant is gone; README and tests follow. Tests first: RED `8e9897f7`, GREEN `933dea74`.
- Why: A110 (`DEC-2026-11-08-perd-39-4-control-plane-callers`: "ops has no path to them") and PERD 39.4 say audit-only; production had mounted it for ops and audit (EDG-01 slice 3c, REQ-AUD-003 reading of 1 Nov). noema#770 raised the conflict; A370 (relayed by the user, not committed: AGENTS 2.3, interim) settles it as audit-only.
- Changed assertions (none deleted or weakened except those about ops): `api_test.go` and `wire_investigations_test.go` asserted that ops reaches the handler; they now assert the role gate's 404 for ops. `boundary_test.go`, `reconstruct_test.go` and `session_auth_test.go` follow the same rule: audit is served; ops, customers and approvers never are, with or without the edge's role gate in front.
- Risks: a caller that relied on ops reading reconstructions now gets 404 (intended). `RoleOps` was an exported constant of `record/reconstruct`; nothing else in the tree used it (the build passes).
- Tests added (REQ-AUD-003, REQ-API-001, INV-12): the route table lists exactly audit for the reconstruction; ops gets 404 through the production build; `mayRead` refuses ops by default.
- Hand mutants, all killed: ops back in the route roles (two cmd/core tests fail); `mayRead` accepting ops (two reconstruct tests fail). Not run: a third (role gate removed), the session was cut by the usage limit.
- Conflicts raised: none left. Debt entries: none.

## Not done: the audit panel route
`GET /v1/investigations/{id}/panel` (PERD 39.4, roles ops and audit in the matrix, POC-FR-17, A351 "the harness polls ... the audit route"). Part 2 of the brief (build it only if an existing specification gives its response contract, roles, read and not-found rule) was not reached: the helper agent was stopped by the usage limit before reporting. Earlier finding (noema#770's report): no response contract, handler, port or store read for it exists in `/contracts`, the PERD, the SoT or the decision records; `ops` in the matrix row also conflicts with A110's audit-only for the reconstruction (the panel row itself says ops and audit).

## Pre-work for the next agent
- Exact next step: decide with the Architect the panel's response contract (`panel.v1`, or `reconstruction.v3`), the store or Record read behind it, and the not-found rule; then build it like `GET /v1/investigations/{id}` (consumer-defined port, session-only tenant and principal, one not-found shape, `NOEMA_READ_TIMEOUT_MS`).
- Files to read first: `services/core/cmd/core/compose.go`, `wire_investigations.go`, `services/core/record/reconstruct/handler.go`, `services/core/edge/authz_matrix_test.go`.
- Do not touch: `/contracts`, `packs/`.
