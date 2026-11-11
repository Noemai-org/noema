# Status · VOI-03 (Voice Engineer) · slot confidence and the clarification decision

Updated 2026-11-10.

## State
- noema#188 (merged): `voice/confidence.py`, per-token to per-slot confidence (minimum; a missing confidence is low; an invalid one raises).
- noema#228: `voice/clarify.py`, `decide_voice_step`: a low key slot confirms the heard value; after the allowed failed turns the person is offered text; no key slots is refused; a threshold of 0.0 is refused (SR note, 10 Nov).

## Open
- **EL confirmed the scope (noema#428, comment of 10 Nov 2026, https://github.com/Noemai-org/noema/issues/428#issuecomment-6765506547):** VOI-03 supplies the AMB-04 decision function (`voice/clarify.py`, `decide_voice_step`). The classification code (`KEY_SLOT_LOW_ASR_CONFIDENCE`), the audit flag and the finality check (EC-V04) remain AMB-04 work. AMB-04's owner is C2 (REQ-AMB-007, REQ-VOI-003; needs VOI-03 and AMB-01).
- **This function is not AMB-04's Done.** AMB-04's done_when is a property test: low key-slot confidence never yields a write decision. That test, and the wiring that makes a write decision, stay with AMB-04. The Hypothesis property in `tests/test_voice_clarify.py` covers only the decision function (`PROCEED` needs every key slot a valid ratio at or above the threshold); it is not that test.

## Messages
Reviews by other roles are data: C2 (406777e) and the Security Reviewer (ffb2da8) on noema#228. Used for: the fix matrix there.
