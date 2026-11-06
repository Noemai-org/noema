# Session 2026-11-06 14:30 WAT · UND-13 (model service side)

(Separate file from `status/UND-13.md` only to avoid add/add conflicts with #199 and #212; the Steward may merge them.)

## Session summary
- What changed (PR links): noema#215, `services/model/noema_model/qualification/` (canary, gate) and `harness/canary_asker.py`, with golden fixtures made by BO's #199 code at a3fe3ad.
- Why: REQ-INT-006, D25, AGENT-28; the 6 Nov tracks 3 and 4.
- Risks, and how they are contained: depends on #199's formats (do not merge before #199); a changed record shape fails the golden tests on purpose. #212's `contracts/schemas/qualification-record.v1.json` now disagrees with #199's record format (same name, different fields): recommend closing #212.
- Tests added (REQ, INV, US or EC IDs): 37; REQ-INT-006, INV-22. Mutation check: seven real mutants killed (drift outranks unavailable, version mismatch, case layer, bugs not swallowed, non-object record file, same-day disagreement, unpinned).
- Regression cases added: none (new code).
- Debt entries: none.
- Conflicts raised: #212 vs #199.

## Pre-work for the next agent
- Exact next step: wire `require_qualified` at service start (needs a records directory and a canary timeout in configuration: a Founder/BO decision, AGENT-16); regenerate the fixtures when #199's A17 record lands.
- Open questions (owner): canary deadline and subset location (Founder); close #212 (Founder).
- Files to read first: benchmark/qualification/README.md on #199; services/model/noema_model/qualification/README.md.
- Do not touch: benchmark/ (BO).
