# TRU-07 · PackProfiles, the production SourceProfiles of the Evidence adapter

State: PR noema#788 (draft), red head `0f4ac968`. Built by Core Engineer 2 (C2). Asked by EL for `wire_trust` (message relayed in session, not recorded as a file).

# Session 2026-11-19 17:00 WAT · TRU-07 profiles

## Session summary
- What changed (PR links): noema#788. `orchestrator/portadapters/trust/profiles.go` (`PackProfiles`, `NewPackProfiles`, `ErrProfileNotFound`, `ErrProfilesData`), `profiles_test.go`, README section.
- Why: `NewEvidence` needs a `SourceProfiles` and none was written (the Evidence status file's "Exact next step"). Without it T9 cannot be wired.
- Risks, and how they are contained: it reads data and decides nothing. Unsound pack data is refused whole at the build; an unknown or unprofiled node is an error, never an empty rule set; answers and inputs are copies.
- Tests added (REQ-TRU-001, REQ-CTX-003, REQ-NFR-002; INV-14, INV-16, INV-23; AGENT-13, AGENT-16): ten tests, a table of twelve refused pack shapes, a composition test with the real Evidence adapter and a test of the real pack against an independent reader. Seen failing first at `0f4ac968` (a stub that answers nothing). Sixteen hand mutants of the implementation were killed; one (a no-op rewrite) is equivalent.
- Regression cases added: none (new feature).
- Debt entries: none new. The double decode of the pack (see README Open) is not debt unless EL declines to expose the slices.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: EL's `wire_trust` calls `trustport.NewPackProfiles(doc.IntentNodes, doc.TrustProfiles)` and `NewEvidence(snapshots, profiles, clock)`.
- Open questions (owner): may a profile require no source (today refused; Architect)? Does EL want `trustpack.Pack` to expose its node and profile slices so the pack is decoded once (EL)?
- Files to read first: `evidence.go` (`SourceRules`, `usable`), `trust-profile.v1.json`, `packs/fintech_v1.json`.
- Do not touch: `trustpack/**` (EL's), `cmd/core/**`.
