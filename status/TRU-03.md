
## Architect's decision, 3 Nov 2026: a CONFIRM never grants authority by itself
- PERD v1.19 §23.4 ("No matching PERMIT: REJECT NO_PERMITTING_RULE (default deny)") nowhere says a CONFIRM can authorize, so the decision is implemented.
- Done: a winning CONFIRM with no PERMIT matched returns REJECT `NO_PERMITTING_RULE` with `Result.ConfirmWithoutPermit` set; the CONFIRM stays in `Applied`. With a matching PERMIT it stays CONFIRM. Test first (commit "TRU-03: failing test first, a CONFIRM alone is denied"), then the fix; the ladder, reference-model and fintech tests were updated to the rule, not weakened.
- Open (Architect): "a distinct reason code". The core reason codes are a closed enum in `/contracts` (PERD §23.3), so a new code needs a PERD change and an AGENT-15 contract bump. Until decided the marker is a field of the result, not a code in the Record.
- Open (Architect, PCK): the shipped fintech policy has `fin.pol.regulatory.complaint_confirm` (CONFIRM) and no PERMIT for `fin.cap.log_complaint_reference`, so a complaint is now denied (fails closed). Recommendation: add a PERMIT rule for it to the pack in a PCK change.
