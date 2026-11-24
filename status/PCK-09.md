# Status · PCK-09

State: green locally, not pushed. Authored By: Engineering Lead (EL).

# Session 2026-11-24 · priority routing pack member (A644)

## Session summary
- What changed: a new pack member `packs/fintech/routing/priority_routing.v1.json` (SLA phase to `{priority, owner_queue}`, SYNTHETIC) with its schema `packs/schemas/priority_routing.v1.json`; the manifest kind `priority_routing` (at most one; `manifest.v1.json` enum, additive); `tools/packcheck` checks it (schema, phases are the pack's `sla_phase`, queues are in `routing_allowlist`); `platform/packload` verifies it and returns `Pack.PriorityRouting`; new package `platform/priorityrouting` (`Parse`, `Table.Lookup`, `Table.Phases`). The fintech manifest is version 3 and pins the file.
- Why: ruling A644 (interim, relayed, not yet committed), with A640: EL supplies only the priority routing as a separate member; C1 builds the evaluator that reads it.
- Risks, and how they are contained: the manifest pin and version lines are also touched by noema#897, #902, #916 and #910 (merge conflict expected); values are SYNTHETIC placeholders, not a policy (AGENT-16); the loader does not parse (the reader does), so a malformed member is refused at wiring, not at load.
- Tests added (REQ-CAP-001; INV-14, INV-21; A551, A616): `platform/priorityrouting/priorityrouting_test.go`, `platform/packload/packload_priority_test.go`, `tools/packcheck/test_priority_routing.py`, changes to `test_manifest.py` and `packs/tests/test_manifest_schema.py`.
- Regression cases added: none (new feature).
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: C1's evaluator calls `priorityrouting.Parse(verified.PriorityRouting)` once at wiring and `Lookup(phase)` per case. An unlisted phase gives NORMAL and no queue; the evaluator must decide the queue for it (open question to the Architect).
- Files to read first: `services/core/platform/priorityrouting/priorityrouting.go`, `packs/fintech/routing/README.md`.
- Do not touch: the manifest `expected` ids, `fintech_v1.json`.
