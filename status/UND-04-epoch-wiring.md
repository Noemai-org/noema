# Status · UND-04 · wire the epoch port and the subject reader (Compose)

State: green held locally, red pushed; no PR is open. Branch `feat/UND-04-epoch-wiring`. Rulings A520, A513, A482 and the note on noema#922 (interim, relayed, not yet committed).

# Session 2026-11-24 00:00 WAT · UND-04

## Session summary
- What changed (PR links): none open. New package `evidence/epochport` (`Setup`, `New`, `Epochs.EpochOf`, `ErrRefused`): implements `interpreter.Epochs` over `portread.Reader.NextEpoch`. New `cmd/core/wire_epoch.go` (`epochParts`: Subject and Epochs together, or neither). `buildUnderstanding` now delegates to `buildUnderstandingWith(..., *epochport.Setup)`; the same pack view feeds the gate and `understanding.SubjectOf`.
- Why: noema#922 added the two optional ports; they come together (`interpreter.New` refuses half). Placement: the adapter must name `adapters.Scope`, and the lint rule `import-adapters` lets only execution and evidence import `adapters`, so it lives under `evidence/` and not in `cmd/core`. `cmd/core` tests cannot name `adapters` either.
- Behaviour: the subject `<vertical>.<kind>:<id>` must have the setup's kind (the pack's transaction resource kind) and one non-empty id with no colon; the id is the transaction reference. The scope is the tenant and principal the Interpreter passes, which come from the verified session (`edgeSessions`), never from a request (INV-12). The read is bounded by `Setup.Timeout`. A list that cannot be relied on is `portread.ErrEpochUnknown`, never 1.
- Both or neither: no setup leaves both unset (epoch 1, no subject: the G1 behaviour). A setup with a part missing (no case port, no kind, no positive timeout, no pack view) is a startup error with fixed text (`cmd/core: the epoch port cannot be built from the verified pack`), naming no value.
- Risks, and how they are contained: Compose passes no setup, so production is unchanged. NAMED GAP: no production case-list reader is built on main (nothing in `cmd/core` builds `adapters.Case`, `portread.Reader` or the Evidence read-and-seal), so Compose cannot supply `Setup.Cases`. When it can, `composeUnderstanding` must pass the setup to `buildUnderstandingWith`. The `/readyz` line for the gap (a fixed text, no value, as A469/A486) needs `compose.go`, which is outside this lane: EL's. The setting for the timeout and the resource kind of a subject (A330: `NOEMA_<AREA>_<WHAT>`, no default) are likewise not added until the reader exists; the kind should come from the pack's descriptor, not a setting (INV-14).
- Tests added (REQ-INT-001, REQ-SEC-004; INV-12, INV-19): `evidence/epochport/epochport_test.go` (real `portread.Reader` over a real `adapters.Case` implementation: scope, id, highest CLOSED + 1, 1 with none closed, unknown on incomplete or failed list, refusals of malformed subjects, another tenant's case ignored, half setups); `cmd/core/wire_epoch_test.go` (neither, both from the same view, half refused with fixed text, the build takes the setup).
- Regression cases added: the same files. Debt entries: none. Conflicts raised: none.
- Mutation check (hand, python): 20 mutants, 17 killed; 3 survivors are equivalent (guards also enforced by `portread.NextEpoch`: empty id, id with a colon, empty tenant or principal).
- Tests needing Postgres and pgsodium (the rest of `cmd/core`, `intentstore`, `interpreter`) fail locally with `schema "pgsodium" does not exist`; they run in CI only.

## Pre-work for the next agent
- Exact next step (EL): when the production case-list reader exists, build `epochport.Setup{Cases: reader, Kind: <the pack's transaction resource kind>, Timeout: <setting>}` in Compose and pass it through `composeUnderstanding` to `buildUnderstandingWith`; add the `/readyz` named gap in `compose.go`.
- Open questions (Architect/EL): does the live case system report status CLOSED (from the noema#922 note)? Where does the subject kind come from (the pack's evidence-source descriptor, `portread.Source.ResourceKind`)?
- Files to read first: `services/core/evidence/epochport/epochport.go`, `services/core/cmd/core/wire_epoch.go`.
- Do not touch: `migrations/`, `intentstore`, `portread`, `interpreter` (merged, C2's).
