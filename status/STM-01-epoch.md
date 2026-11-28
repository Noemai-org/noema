# Status · STM-01 · the EPOCH runtime source of an outcome binding (Core Engineer 2)

Updated 2026-11-28. All data is SYNTHETIC.

## State
- Orders: A740 (the `EPOCH` source of `outcome_bindings`: the subject's current logical-resolution epoch, read through the same `NextEpoch` port the Interpreter uses; the contract member is C1's, in #1026) and A776 (the runtime source and the port are C2's, because C2 built the request package). A776 said "STACKED ON #1019": #1019 merged at 12:16 on 28 Nov, so this is cut from `main` and is not stacked. PR noema#1073.

## What it does
| Part | Change |
|---|---|
| `request.FromEpoch` (`"EPOCH"`) | a fifth `SourceKind` (the closed set grows by one; the contract's name for it is C1's). It names no param. `Build` binds it to the epoch in `Inputs.Epoch` as decimal text; a nil, zero or negative epoch, or a source that names a param, is `ErrBinding`: an epoch is never 1 by default (INV-19). Only an EPOCH binding reads it |
| `request.Epochs` | the port, with the Interpreter's own shape: `EpochOf(ctx, tenantID, principalID, subjectRef) (int, error)` (`evidence/epochport.Epochs` implements it) |
| `GrantRequests.For` | asks the port only when the pack's outcome has an EPOCH binding, only after the grant has passed everything `Build` checks (`SubjectOf` runs the same checks first, so a refused grant costs no case-list read), as the principal the grant was issued to in the grant's tenant (INV-12), for the subject the outcome names. No port wired for such an outcome is `ErrIncomplete`; a port error is returned with its cause; an epoch below 1 is `ErrBinding` |
| `cmd/core` | `outcomeRequestSeams.Epochs` is an optional seam passed to every tenant's source; its absence is not a gap of the build |

## Open points (for EL, who composes, and the Steward)
1. **The subject's form.** The port (`epochport.EpochOf`) accepts a `<vertical>.<kind>:<id>` reference and refuses anything else with `ErrRefused`. `For` passes the value the pack's subject variable is bound to. If the pack binds the subject to a bare id, every EPOCH request is refused (fail closed). Which form the pack's subject binding carries is the pack's (#1026); nothing here guesses.
2. **When the epoch is read.** The epoch is read when the request is built, after the write. `NextEpoch` gives the epoch of a new intent about the subject; a case closed between the write and the verification would change it. A740 names this port and this moment; the stored `logical_resolution_epoch` of the admitted intent would not move. Decision needed from the Architect if the stored one is wanted instead.
3. Composition of `epochport.New` over the case-list reader in `cmd/core` is EL's (the Interpreter's composition holds the same object).

## Pre-work for the next agent
- Exact next step: EL passes `epochport.Epochs` as `outcomeRequestSeams.Epochs` where the Interpreter's `Epochs` is composed; C1's #1026 names `EPOCH` in the pack contract.
- Do not touch: `outcome/verifier` (the request it receives is unchanged in shape).
