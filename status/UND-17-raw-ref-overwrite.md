# UND-17-raw-ref-overwrite

State: red pushed, green committed locally, not pushed.

# Session 2026-11-20 WAT · UND-17

## Session summary
- What changed (PR links): the core overwrites `source.raw_ref` with the INPUT_RECEIVED object_ref (`req_<ULID>`, A184), read from the stored event, between Compile and Admit (noema#580; A219, interim, relayed, not yet committed). Draft PR: see the PR for this branch.
- Why: the compiler leaves `raw_ref` as the unset marker; nothing wrote the core's value before the intent was stored.
- Risks, and how they are contained: the port gains one Admissions lookup per turn (second read of the chain after the runner's); a refusal is fail-closed before the model is called. Mutants are listed in the PR.
- Tests added (REQ, INV, US or EC IDs): REQ-INT-001, REQ-INT-002, INV-02, INV-12, AGENT-09.
- Regression cases added: none (new behaviour).
- Debt entries: the double Admissions lookup (runner and port); a later change could hand the runner's Admission to the Interpreter.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: wire `interpreterport.Config.Admissions` in cmd/core (not done here: noema#773 and #812 own cmd/core).
- Open questions (owner): none.
- Files to read first: `services/core/understanding/interpreter/interpreter.go`, `services/core/orchestrator/portadapters/interpreter/port.go`.
- Do not touch: `cmd/core` from this PR.
