<!-- Copy of Noemai-org/docs docs/poc/perd/templates/task-card.md at docs commit e31a82f56a897866c8ed5f842852abb44a0115cb. The docs original wins on any difference. Re-point by copying it again. -->

# Task card template

<!-- Source: PERD §61.4 (NOEMA_POC_PERD_v1.1.md lines 5438–5453; current: see the PERD README), copied verbatim. Copy the block into the task or PR, fill every <…>, keep the field names unchanged. -->

Every task given to a coding agent uses this card (PERD §61.4, REQ-AGT-002). The block below is the PERD's card, unchanged.

```
TASK-<step>-<nnn>  <title>
Priority: P0|P1|P2
Requirement IDs: REQ-…, SoT IDs (INV-…, POC-FR-…, US-…)       ← AGENT-01: no ID, no merge
Rationale: <one paragraph from the REQ>
Modules touched (allowlist): services/core/execution/…, contracts/…   ← nothing else may change
Contracts: <schema files and versions>; bump required? Y/N (AGENT-15)
Inputs/outputs: <types>
Acceptance criteria: <copied SHALL statements>
Tests to write FIRST (AGENT-11): <property/contract/benchmark case IDs>
Faults to exercise (AGENT-12): <FaultPlan IDs>
Dependencies: TASK-…, REQ-…
Out of scope: <explicit list>
Definition of done: tests green; boundary/lint green; Record events asserted; docs updated; PR answers the SoT §52 questions
If blocked or conflicting: STOP and emit the CONFLICT block (§71.3)
```

## How to fill each field

<!-- One line per field. Where a field cannot be filled, write TODO(owner) and the reason; do not invent IDs. -->

| Field | Instruction |
|---|---|
| `TASK-<step>-<nnn>  <title>` | `<step>` is the PERD build step (P0–P14, PERD §61.2); `<nnn>` is a running number within that step. |
| `Priority` | The priority of the main REQ, from [`../requirements-index.csv`](../requirements-index.csv). |
| `Requirement IDs` | At least one PERD REQ and the SoT IDs it cites (the `sot_ids` column of the index); no ID, no merge (AGENT-01, REQ-AGT-001). |
| `Rationale` | One paragraph taken from the REQ's Rationale line; add nothing new. |
| `Modules touched (allowlist)` | Every path the agent may change; CI fails on any other path (REQ-AGT-002). |
| `Contracts` | Schema files and versions touched; say whether a version bump is needed (AGENT-15). |
| `Inputs/outputs` | The types consumed and produced, by contract name. |
| `Acceptance criteria` | Copy the REQ's SHALL statements verbatim. |
| `Tests to write FIRST` | Property, contract and benchmark case IDs, written before the code (AGENT-11). |
| `Faults to exercise` | FaultPlan IDs from the environment, never mocks (AGENT-12, PERD §37). |
| `Dependencies` | Other task cards and REQs (the `dependencies` column of the index). |
| `Out of scope` | An explicit list of what this task must not do. |
| `Definition of done` | Keep as written; the PR answers the SoT §52 questions. |
| `If blocked or conflicting: STOP` | Emit the [conflict block](conflict-block.md) in the PR and stop (REQ-AGT-003, AGENT-27). |
