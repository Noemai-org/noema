## Task, REQ and SoT IDs
- Task ID: <task ID, for example the one on the board>
- PERD REQ IDs: <the REQ blocks the card cites>
- SoT IDs: <the INV, BD, PLAT or POC-FR IDs those REQs cite, or a section number>

## Plan
Written before the code (AGENTS section 3, step 6). For a P0 path (Trust, Execution, Outcome, Record, contracts, packs) this is required.
- Files touched (inside the card's allowlist):
- Tests written first (seen failing):
- Faults exercised (FaultPlans or the hostility proxy, never mocks):
- Record events asserted:

## SoT section 52 questions
1. <answer each of the five questions of SoT section 52>
2. Does this increase or decrease the unsafe autonomy surface?

## Checklist
- [ ] Diff is inside the allowlist
- [ ] Tests, lint and type checks pass locally (paste the output; a claim without output is not evidence)
- [ ] No secret, no real data; fixtures labelled SYNTHETIC
- [ ] Generated files regenerated, not edited
- [ ] Docs and the status file updated
- [ ] A bug fix names its regression case
- [ ] Not stacked on another PR unless the title says so (two deep at most)

## Hand-off report
Template: docs repository, docs/poc/execution/templates/agent-handoff.md. What changed, what was verified (with the check-run link at the final head), what is open, the next step.

## CONFLICT DETECTED
Leave this section out when there is no conflict. Otherwise stop work on the conflicting part and fill the block (the PERD conflict-block rule; template in docs/templates/conflict-block.md).
```
CONFLICT DETECTED
Affected IDs: <SoT IDs, REQ IDs, SOT-C-xx if listed>
Statements in conflict: <quote A (source, section)> vs <quote B (source, section)>
Evidence: <code, test output, or document lines>
Proposed alternatives: <1..n, each with blast radius>
Safer reading applied meanwhile (AGENT-20): <which>
Decision required from: <founders | tech lead | benchmark owner>
```
