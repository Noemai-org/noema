# Status · EXP-01: the answer builder (noema#618)

Updated 2026-11-16. Owner: EL. Ruling A237 (interim; docs#187; relayed; §3 not committed).

# Session 2026-11-16 · the answer builder sets done.verified and case_ref_masked

## Session summary
- What changed (noema#618): the package `explanation/answer`: `Build(Input) (contracts.CustomerAnswerV1, error)` and `Verified([]Verdict) bool`. `done.verified` is true only when there is at least one verdict and every verdict is ACHIEVED. `case_ref_masked` is present only when the case-opened outcome (the pack's ID, an input; never a literal here, INV-14) has an ACHIEVED verdict and every verdict of that outcome is ACHIEVED; the reference must already be masked (a mask mark, at most four digits in all, at most 64 characters). Every text member is held to the contract, and a text that breaks it is an error, never masked or trimmed.
- Why: noema#611 (FE): the UI must not derive verification or the case reference; the Founder's direction recorded there is that the core sets them. A237 says "noema#611 as in §3"; §3 was not given to the Steward and is not committed, so this implements what the Founder relayed in the session and what noema#611 records. If §3 says more, this PR changes.
- Risks, and how they are contained: a verified claim or a case reference shown without its verdict. Contained by withholding: the flag is false and the reference absent unless the rule holds; with no outcome configured there is no reference; an empty outcome ID never matches; a raw reference is refused.
- Tests added (REQ-EXP-001, REQ-EXP-002, REQ-SEC-003, REQ-UI-001; INV-13, INV-14, AGENT-13): eight. Red first at `d1197e5f` (the package had no non-test file). Eight mutants killed (the empty-verdict rule; one ACHIEVED enough; any case-opened verdict enough; the empty outcome ID; the digit count; the mask mark; the digit guard; the item bound). Two tests (an empty outcome ID; digits separated by letters) were added in the fix push after the first mutant run showed the first version of the tests missed them; stated, not hidden.
- Regression cases added: the same. Debt entries: none. Conflicts raised: none.

## Not done, stated
The sources of `found`, `not_known`, `advice` and `next` and the call site: nothing builds a `last_answer` yet. The facts source (noema#586, #595) and the explanation text feed it; the investigation route and the events route carry it.

## Pre-work for the next agent
- Exact next step: the investigation route builds `last_answer` with `answer.Build` from the Record's verdicts (`VERDICT` events of the correlation, outcome ID from the event) and the case reference from the RECEIPT's `external_ack`, masked; pass the pack's case-opened outcome ID.
- Files to read first: `explanation/answer/answer.go`, `contracts/schemas/customer-answer.v1.json`.
- Do not touch: `internal/contracts` (generated).
