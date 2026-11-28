# Session 2026-11-28 · STM-05 · A696 (C1, interim, relayed): the recovery write path

## Session summary
- What changed: `cmd/core/recovery_write.go`: `buildRecoveryWritePorts(records, parts, seams)` gives the write ports recovery needs: Outcome's Verifier (`verifierport.Port`) over the request reader of noema#1019 (`buildOutcomeRequests`, per tenant on that tenant's outcome login), the Journal over the Record reader (`tenantJournal`), the Trust adapter's Bindings, and `recovery.NoDispatch` as the Executor. A part that is missing is a named gap in fixed text and the zero `WritePorts` is returned (never a part-built set).
- Why: `status/STM-05-recovery-reader.md` named the recovery write path as blocked on slice 4's request reader and on the dispatcher (noema#976). #1019 has merged, and recovery never dispatches, so it does not need the dispatcher at all: its Executor is `recovery.NoDispatch` and `buildRecoverySurface` already substitutes it.
- Risks, and how they are contained: nothing here can spend (the Executor refuses with `recovery.ErrNoDispatch`; a test proves the Record, grant spends, receipts and grants are unchanged by a verify and a refused Execute). The request reader is per tenant; tenant B asking for tenant A's grant gets `request.ErrNotFound` and the verifier is not asked.
- Tests added (REQ-VER-004, REQ-VER-001, REQ-SEC-004; INV-03, INV-04, INV-07, INV-12, AGENT-08): `cmd/core/recovery_write_test.go`, on a real migrated PostgreSQL with each tenant's own outcome and Record-read logins and a receipt signed through `receipt_insert`. Regression cases: the red at `f28fc8349`. Debt entries: none. Conflicts raised: none.

## Named gaps (not guessed)
| Gap | Owner | Why it is still missing |
|---|---|---|
| Nothing calls `buildRecoveryWritePorts` yet | EL | `SurfaceSeams.Write` and `SurfaceSeams.Recovery` are never set in the composition root (`compose*.go` is EL's), so `/readyz` still names "recovery: reader not built". |
| Outcome's verifier (`verifierport.Verifier`) | EL / OUT lane | No composition of `outcome/verifier.New` (clock, waiter, ids, cases, store, signer, authenticity, scope, budget) exists in `cmd/core`. |
| `outcomeRequestSeams.Outcomes` | pack owner | The pack's `outcome_bindings` (noema#1026, A723, A740, A774) are not on main; `request.Outcomes` has no production implementation. |
| `outcomeRequestSeams.Signatures` and `.IDs` | EL | The receipt signature checker (`outcome/receiptsig`) and the `vrf_` id source are not composed in `cmd/core`. |
| Trust `Bindings` | EL | The Trust adapter (`trustport.Port`) is not composed with its evaluator, intents and Record reads. |
| A Journal that sees a spent grant | none (works) | The test world seeds the grant spend rows directly; the GRANT_SPENT event comes from the Execution ledger, so the Journal test asserts the empty-Record case and the per-tenant read only. |
| Receipts written before A692 carry no `step_params` | Execution | A bound param they lack is `request.ErrBinding` (typed refusal, never an empty binding). |

## Pre-work for the next agent
- Exact next step: EL sets `SurfaceSeams.Write` for recovery from `buildRecoveryWritePorts` (the Verifier seam, the three request seams, the Trust Bindings) and `SurfaceSeams.Recovery` from `recoverypg.New(...)`.
- Files to read first: `cmd/core/recovery_write.go`, `cmd/core/compose_surfaces_recovery.go`, `orchestrator/recovery/recover.go`.
- Do not touch: `cmd/core/compose*.go` (EL's composition), `outcome/verifier`, `execution/`.
