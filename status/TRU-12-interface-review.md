# Session 2026-11-24 · TRU-12 · the Trust interface is a typed request, not a conversation (REQ-TRU-011; G1, A449)

## Session summary
- What changed (PR links): the guards of `services/core/trust/intake/typed_request_test.go` and this note. No production code changes.
- Why: TRU-12's "Done when" is an interface review note signed by EL, so that a later Mode 2 can call Trust with a typed request and nothing else (POC-FR-16: Mode 2 is not built; keeping the interface clean is required, AGENT-03).
- The interface (as built, at main `cc992c3b` and later):
  - **In:** `intake.Request{Session{TenantID, PrincipalID, Role}, CorrelationID, IntentRef, EvidenceSnapshotID, EvidenceUnavailable}`. The session comes from the authenticated session (INV-12). The intent is a reference, `int_<id>@<version>`, to a stored, admitted Intent Object that Trust reads through its port; Trust receives no utterance, no transcript, no model text and no conversation state.
  - **Out:** `intake.Result{Decision, Trace, DecisionID, GrantIDs, ConfirmationRequestID}`: the one decision of PERD 23.3 and the IDs the Writer returned. Nothing in it is conversational.
  - **Continuations:** `intake.ContinuationRequest` names a capability, facts, a packet reference and the disposition ID it follows: typed fields, no text.
  - **Ports:** everything outside Trust (the intent store, the registry, the pack, the evidence layer, the planner, the Recorder) is reached through the small interfaces of `ports*.go`; Trust imports none of those packages.
- Evidence it stays so: `TestTheRequestIsATypedRequestAndNotAConversation` holds the exact member set of `Request` and `Session` (a new member, above all a text member, fails it and needs this note to change); `TestNoTrustSourceImportsTheConversationSideOrAnotherPower` parses every non-test source under `services/core/trust` and refuses an import of the orchestrator, the edge, the input gateway, the understanding or evidence layers, execution, outcome, or the explainer. Hand mutants, both seen failing: an `Utterance string` member on `Request`; an import of the orchestrator in `trust/clocks`. Both reverted.
- Risks, and how they are contained: the guards fix the present member set, so a legitimate new typed member is a deliberate edit of the test and of this note; that is the intent.
- Tests added (REQ-TRU-011; INV-01, INV-12): the two above. Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Signature
- EL review of the interface above: **signed, EL, 2026-11-24.** I read `intake.Request` (Session, CorrelationID, IntentRef, EvidenceSnapshotID, EvidenceUnavailable) and `intake.Session` (TenantID, PrincipalID, Role) in `trust/intake/ports.go` on current `main` merged into this branch, and the guard's member set matches them exactly. Every member is typed and none carries text, a transcript or conversation state. I ran both guards with `-race` and they pass. The import-boundary guard lists the orchestrator, edge, input gateway, understanding, evidence, execution, outcome and explanation, which are the conversation side and the other powers (INV-01, SoT 51). Limit, as SR noted: a text value inside an existing string member such as `IntentRef` is for the schema gate to refuse, not this guard. This is the Done-when review of TRU-12 (REQ-TRU-011); it is not an approval of the PR and I did not merge.

## Pre-work for the next agent
- Exact next step: SR and one non-author Founder read; the board status of TRU-12 moves only on merged evidence (the Steward).
- Do not touch: `ports.go` members of `Request` without changing the guard and this note in the same PR.

## Amendment 2026-11-25 (C1, TRU-10, A581, A590): one typed member, `Request.TurnID`
- What: `intake.Request` gains `TurnID string`: the client's request ID of the turn the request answers, a lower-case UUID (canonical-input `request_id`), a typed value and no text. The guard `TestTheRequestIsATypedRequestAndNotAConversation` lists it.
- Why: A581 and A590 key an early (null-basis) decision by the turn, the verified principal and the tenant, so a replay of the same turn returns the stored record and a new turn records a new one. Trust reads it only as that key, never as an instruction; it is shape-checked by the Recorder's adapter and the Writer and by the database (`decision_record_early_key_shape`).
- Not changed: `Session`, the other members, the import-boundary guard. No utterance, transcript or conversation state enters Trust.
- Signature: **signed, EL, 2026-11-25.** I read `intake.Request` with the new `TurnID string` in `trust/intake/ports.go` and the amended member-set guard: `TurnID` is a typed string, shape-checked to a lower-case UUID by the Recorder adapter, the Writer and the database (`decision_record_early_key_shape`), and Trust uses it only as the key of an early decision (A581, A590). It carries no utterance, transcript or conversation state, and `Session` and the import-boundary guard are unchanged. This is the Done-when re-signature for the member-set change of REQ-TRU-011; it is not an approval of the PR and I did not merge.
