<!-- Copy of Noemai-org/docs docs/poc/perd/templates/conflict-block.md at docs commit e31a82f56a897866c8ed5f842852abb44a0115cb. The docs original wins on any difference. Re-point by copying it again. -->

# Conflict block template

<!-- Source: PERD REQ-AGT-003 (NOEMA_POC_PERD_v1.1.md lines 5831–5839; current: see the PERD README), copied verbatim. Paste it into the PR description, fill every <…>, and stop work on the conflicting part (AGENT-27). -->

When the specification and the implementation conflict, or the PERD and the SoT conflict, the agent stops and puts this block in the PR (REQ-AGT-003, AGENT-26, AGENT-27). Check [`../conflicts-register.md`](../conflicts-register.md) first and cite the `SOT-C-xx` ID if the conflict is already listed.

```
CONFLICT DETECTED
Affected IDs: <SoT IDs, REQ IDs, SOT-C-xx if listed>
Statements in conflict: <quote A (source, section)> vs <quote B (source, section)>
Evidence: <code, test output, or document lines>
Proposed alternatives: <1..n, each with blast radius>
Safer reading applied meanwhile (AGENT-20): <which>
Decision required from: <founders | tech lead | benchmark owner>
```

## Example: SOT-C-12 (freeze scope)

<!-- Illustrative only. It shows the shape of a filled block; it decides nothing. SOT-C-12 stays Open until a decision record changes it. -->

```
CONFLICT DETECTED
Affected IDs: INV-20, US-03, US-14, REQ-RES-002, SOT-C-12
Statements in conflict: "A CONTRADICTORY_STATE classification freezes all consequential writes on that target until Trust records a resolution or escalation." (SoT §45, INV-20) vs "freezes consequential writes on that transaction, records the evidence, and escalates with an evidence packet." (SoT §37, US-14) and "While it exists, Trust may issue grants only for capabilities whose descriptor declares `allowed_under_freeze: true` [REC]" (PERD §22.3, REQ-RES-002)
Evidence: PERD §72 SOT-C-12: "Literal reading blocks the required escalation"; docs/poc/perd/conflicts-register.md shows SOT-C-12 as Open
Proposed alternatives: (1) adopt the PERD freeze allow-list (escalation case, evidence record, route) as pack data and clarify the INV-20 wording; blast radius: SoT INV-20 text, the fintech pack, REQ-RES-002 tests. (2) keep INV-20 literal and escalate by handoff only on a frozen target; blast radius: US-03 and US-14 acceptance, the escalation-case path
Safer reading applied meanwhile (AGENT-20): refuse every consequential write on the frozen target; escalate by handoff until a decision record settles SOT-C-12
Decision required from: founders
```
