# Status · PCK-09

State: green locally, not pushed. Authored By: Engineering Lead (EL).

# Session 2026-11-24 · priority routing pack member (A644)

## Session summary
- What changed: a new pack member `packs/fintech/routing/priority_routing.v1.json` (SLA phase to `{priority, owner_queue}`, SYNTHETIC) with its schema `packs/schemas/priority_routing.v1.json`; the manifest kind `priority_routing` (at most one; `manifest.v1.json` enum, additive); `tools/packcheck` checks it (schema, phases are the pack's `sla_phase`, queues are in `routing_allowlist`, every `sla_phase` has an entry, A660); `platform/packload` verifies it and returns `Pack.PriorityRouting`; new package `platform/priorityrouting` (`Parse`, `Table.Lookup`, `Table.Phases`). The fintech manifest pins the file; the kind is additive and carries no version gate (the manifest is version 4 on main since A645).
- Why: ruling A644 (interim, relayed, not yet committed), with A640: EL supplies only the priority routing as a separate member; C1 builds the evaluator that reads it.
- Risks, and how they are contained: the manifest pin and version lines are also touched by noema#897, #902, #916 and #910 (merge conflict expected); values are SYNTHETIC placeholders, not a policy (AGENT-16); the loader does not parse (the reader does), so a malformed member is refused at wiring, not at load.
- Tests added (REQ-CAP-001; INV-14, INV-21; A551, A616): `platform/priorityrouting/priorityrouting_test.go`, `platform/packload/packload_priority_test.go`, `tools/packcheck/test_priority_routing.py`, changes to `test_manifest.py` and `packs/tests/test_manifest_schema.py`.
- Regression cases added: none (new feature).
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: C1's evaluator calls `priorityrouting.Parse(verified.PriorityRouting)` once at wiring and `Lookup(phase)` per case. An unlisted phase gives the zero Entry and false (A660, fail-closed); treat it as a fault. packcheck guarantees every clock status has an entry.
- Files to read first: `services/core/platform/priorityrouting/priorityrouting.go`, `packs/fintech/routing/README.md`.
- Open question (Architect): the vocabulary for "every phase" is `predicate_variables.sla_phase`, tied to the `trust/clocks` `Phase` constants by a test; EL's reading of A660, pending confirmation.
- Do not touch: the manifest `expected` ids, `fintech_v1.json`.

# Session 2026-11-26 · parser refuses `note: null` (A678)

## Session summary
- What changed: `platform/priorityrouting.Parse` read `note` as `*string`, so `"note": null` decoded to nil and was accepted. `note` is now read raw and must be a JSON string of 1 to 4000 bytes when present. main was merged: the manifest keeps its version 4 and gains the `priority_routing` kind beside `limits` and `injection_patterns` (no new version; the pin of the routing file is unchanged).
- Why: A678 (interim, relayed, not yet committed): the parser refuses `note: null`, because the schema requires a string when present.
- packcheck: already refused `note: null` (the schema says `"type": "string"`); `test_a_null_member_fails` now pins that, for `note` and for every other member.
- Other optional members with the same hole: none. `note` is the only optional member. Every other member is required and a null in it was already refused (table id, label, version, phases, entry, priority, owner queue); tests now cover each.
- Tests added (REQ-CAP-001; INV-21; A678): `TestAMemberThatIsNotExactlyItsShapeIsRefused` (null and non-string note, empty and over-long note, nulls in each required member), `TestTheNoteIsOptionalAndBounded`; `test_a_null_member_fails` in `tools/packcheck/test_priority_routing.py`.
- Regression cases: the `note null` case. Debt: none. Conflicts: none.

## Pre-work for the next agent
- Exact next step: push the red, read its job, push the green (EL).
- Do not touch: the manifest pin of the routing file.
