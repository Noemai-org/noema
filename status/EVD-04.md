# Status · EVD-04 (Core Engineer 2) · the principal-scoped candidates read

Updated 2026-11-24.

## State
- A645 (interim, committed in `DEC-2026-11-24-architect-rulings-a644-a656`) gives the Candidates read (REQ-CTX-006) to C2 as EVD-04. Needs: EVD-01 Done; UND-01 is "In progress" on the board while the model service is merged: the Founder's order is the waiver.
- Built: `evidence/candidates_api` (`Reader`, `New`, `Config`, `MaxCandidates`, typed errors), tests first. Red CI read before the green.

## Open
- OPEN entry text for the Steward: name masking. candidates-response.v1 says "never an unmasked name"; no document says what a masked name is. Safest default built: names pass as the source gave them and only digit runs are masked.
- Whether the response should carry a completeness marker: today more than 20 matches is `ErrTooMany` and an unmarked page is `ErrIncomplete` (an error, never a cut list). The intake maps a read error to UNKNOWN; the PERD says more than four candidates escalates. Architect.
- No Record event: no evidence-source descriptor exists for the candidate search (SR's question on noema#741 stays open).

## Messages
- 2026-11-24 · Founder → C2: "you own the Candidates read (EVD-04) and the injection-screen patterns" (A645).

# Session 2026-11-24 · EVD-04

## Session summary
- What changed (PR links): the candidates read (this PR).
- Why: A645.
- Risks, and how they are contained: a leak across principals (the scope is the request's; the test world scopes rows by it), a leak of digits or names (masking, fixed error text), a cut list (refused whole).
- Tests added (REQ, INV, US or EC IDs): REQ-CTX-006, REQ-AMB-002, REQ-SEC-003; INV-01, INV-12, INV-21; US-07 (scope).
- Regression cases added: none (no bug fixed).
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: EL composes the endpoint and passes the real `adapters.Transactions`, the masking limits and the pack's resource kind to `candidates_api.New`; the Ranker (noema#918) and the Labeller (EL) complete the trust adapter's parts.
- Files to read first: `evidence/candidates_api/README.md`, `orchestrator/portadapters/trust/candidates.go`.
- Do not touch: the red tests' meaning.
