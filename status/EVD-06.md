# Status · EVD-06 · event time, ingestion time and enterprise strings

Owner: C2 · Task: EVD-06 (REQ-CTX-005, REQ-CTX-007). Date: 2026-11-17.

# Session 2026-11-17 · EVD-06 slice 2 · the advisory injection detector (REQ-CTX-007, ruling A292)

## Session summary
- What changed (PR links): noema#688. New package `services/core/evidence/injection/` (loader, detector, `Screener`, the `Recorder` port and a `RecordRecorder` adapter, README) and the pack data `packs/fintech/context/injection_patterns.v1.json` (SYNTHETIC, 8 patterns). No contract, migration, dependency, workflow or configuration change.
- Why: REQ-CTX-007 asks for a heuristic injection detector with the pattern list in pack data, advisory only, auditing `SECURITY_EVENT:INJECTION_DETECTED`; ruling A292 puts it in the core Evidence layer.
- Risks, and how they are contained: advisory only, so a false positive is noise and a miss is not a safety failure (SoT §29). A finding is a field name and a pattern id and never text (AGENT-13): the event builder refuses a field name or id that is not an identifier, so a value passed as a name is not recorded. The loader refuses any pattern that can match the empty string, so no pattern flags every field. RE2 only (linear time), and a value is cut to the cap before any pattern runs.
- Tests added (REQ-CTX-007, REQ-SEC-007, REQ-AUD-001, INV-11, INV-12, AGENT-09, AGENT-13, US-09, EC-X02, EC-X03, EC-X04, EC-E16): loader refusals at their edges, 32 injection strings and 38 benign strings, truncation before detection, a 1 MiB bound, the screener and recorder behaviour, the event shape through the chain's own validation and payload guard, and database tests (CI only).
- Regression cases added: none (new code).
- Debt entries: none added.
- Conflicts raised: none.

## Evidence
- Red run: the tests were pushed first (e48f1407); locally they did not build (the package did not exist).
- Local: the tests that need no database pass. The `TestDB...` tests were run here and fail at the first database insert with `schema "pgsodium" does not exist`, as `TestBuilderPersistsThePayloadTheItemAndTheRecordEvent` does on `main` in this sandbox; they run in CI.
- Mutants: 52 hand mutants over the loader, detector, screener and event builder (comparisons flipped, validation dropped, truncation skipped or off by one, the matched text returned, control characters kept or counted, the recorder's kind, emitter and checks changed). 50 killed; 6 first survived and tests were added for 4 of them (version zero with a matching expectation, a regex compiled as written, any number of control characters, the recorder's refusal text). 2 are equivalent and stay: the repeated-member check by lower-cased key sits behind the exact-member check, and the empty-regex check sits behind the empty-match check. The database half of `RecordRecorder.Record` (begin, append, rollback, commit and their order) is not reachable by a local test; its mutants are for CI.

## Open questions
- The detector's call site: nothing calls the `Screener` yet; the evidence read path and the composition wire it in (Architect, next slice).
- Where the 256-character cap and the loader bounds live in configuration (`platform/config` has no entry): the caller supplies `Limits`; a threshold is not decided here (AGENT-16).
- The event's `kind` is `INJECTION_DETECTED` (the contract's `kind` member of SECURITY_EVENT, `record-event.v3.json`); the brief and PERD call it a "reason".
- The object the event is on: the caller supplies `Subject.ObjectRef` (for example the evidence item, `evr_...`). Whether a read that has no item yet should use the correlation's object is the Architect's call.
- Control characters are removed before the cap is applied (not after), so the detector sees at least what a prompt would hold. If the data-block builder caps first, nothing changes for safety; the order should be made one rule when that builder is written.
- A truncation with no finding is returned in `Result.TruncatedFields` and is not recorded here: "marked truncated and recorded" belongs to the data-block slice.

## Pre-work for the next agent
- Exact next step: wire `Screener` into the evidence read path (after the item is stored, with the item's id as `ObjectRef`) and read `Limits` from configuration; build the data block of REQ-CTX-007.
- Files to read first: `services/core/evidence/injection/README.md`, `packs/fintech/context/injection_patterns.v1.json`, `services/core/evidence/layer/builder.go`.
- Do not touch: the pattern list's ids without a version bump; they are in the Record.
