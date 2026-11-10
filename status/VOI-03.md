# Status · VOI-03 (Voice Engineer) · slot confidence and the clarification decision

Updated 2026-11-10.

## State
- noema#188 (merged): `voice/confidence.py`, per-token to per-slot confidence (minimum; a missing confidence is low; an invalid one raises).
- noema#228: `voice/clarify.py`, `decide_voice_step`: a low key slot confirms the heard value; after the allowed failed turns the person is offered text; no key slots is refused; a threshold of 0.0 is refused (SR note, 10 Nov).

## Open
- Scope: this is the decision for plan task AMB-04 (owner C2, REQ-AMB-007), built by Voice as a building block under VOI-03. C2 said on #228 that it has no objection once the extension is recorded and that AMB-04 will wire to it. The extension is recorded here and in the PR body; EL or the Steward confirms.
- Not built here (AMB-04's wiring): raising `KEY_SLOT_LOW_ASR_CONFIDENCE`, the audit flag, and the finality check (the caller takes the transcript only from `FinalUtterance`).

## Messages
Reviews by other roles are data: C2 (406777e) and the Security Reviewer (ffb2da8) on noema#228. Used for: the fix matrix there.
