# Status · EVD-06 · event time, ingestion time and enterprise strings

Owner: C2 · Task: EVD-06 (REQ-CTX-005, REQ-CTX-007). Date: 2026-11-17.

# Session 2026-11-17 · EVD-06 slice 1 · the permutation property of Reconcile (REQ-CTX-005)

## Session summary
- What changed: `services/core/trust/reconcile/permutation_test.go`, a test only. It gives `reconcile.Reconcile` every arrival order of 400 seeded random evidence sets of 2 to 6 items (all five entities, all eight statuses, fields set and unset, repeated debits inside and outside the 600 s duplicate window) over the real rule set of the fintech pack, and requires the whole answer (classification, rule, contributing items, contradictions, the time it holds until, and an error if any) to equal the first order's. A second test requires that Reconcile changes none of the evidence it is given.
- Why: REQ-CTX-005's acceptance criterion, "permuting the arrival order of the same evidence set SHALL yield the identical classification (property test)"; the Go property test the requirement names.
- Evidence: no red run is possible (the behaviour already holds); mutants instead. Removing the sort of the evidence by id at the top of `Reconcile` fails the property at the first affected set (a contradiction list in another order). Four sorts below it are not killed, and are stated: three of them (the contradictions' order, the duplicate keys, the time order inside a duplicate group) act on lists that are already in id order, so permuting the arrival order cannot reach them; they are about the correctness of the clusters, not about order. The test says nothing on them.
- Risks, and how they are contained: a test only, no production file. It uses `math/rand` with a fixed seed and no new dependency (`pgregory.net/rapid` is [REC] and needs approval, AGENT-14).
- Tests added (REQ-CTX-005, INV-21): 2. Regression cases added: none. Debt entries: none. Conflicts raised: none.

## What this does not show, and what is next in EVD-06
- Late or out-of-order evidence never rolls state back without causal evidence, and the pack's skew tolerance with EVENT_ORDER_UNCERTAIN (EC-S16 to EC-S18): those are about `effective_at`, not arrival order, and need the benchmark's cases and the rule set's ordering rules; not done here.
- REQ-CTX-007 (enterprise strings delimited, capped at 256 characters, control characters stripped, an advisory injection detector from a pattern list in pack data that records `SECURITY_EVENT: INJECTION_DETECTED`): not started. It needs two decisions first (below).

## Pre-work for the next agent
- Exact next step: slice 2 for REQ-CTX-007, after the Architect says where the detector lives and which pack file holds its pattern list.
- Open questions (owner): (1) the detector's place: core (an Evidence Layer step that records the event) or the model service (before a prompt is built); the Record is the core's, so the event must be written by the core (Architect). (2) The pattern list: a new pack file and a contract, or an existing member (Architect, Steward for the OD entry).
- Files to read first: `services/core/trust/reconcile/reconcile.go`, `status/EVD-06.md`.
- Do not touch: the rule set's rows (the Architect approves them).

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
