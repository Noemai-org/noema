# Session 2026-11-24 · TRU-12 · the Trust interface is a typed request, not a conversation (REQ-TRU-011; G1, A449)

## Session summary
- What changed (PR links): the guards of `services/core/trust/intake/typed_request_test.go` and this note. No production code changes.
- Why: TRU-12's "Done when" is an interface review note signed by EL, so that a later Mode 2 can call Trust with a typed request and nothing else (POC-FR-16: Mode 2 is not built; keeping the interface clean is required, AGENT-03).
- The interface (as built, at main `cc992c3b` and later):
  - **In:** `intake.Request{Session{TenantID, PrincipalID, Role}, CorrelationID, IntentRef, EvidenceSnapshotID, EvidenceUnavailable}`. The session comes from the authenticated session (INV-12). The intent is a reference, `int_<id>@<version>`, to a stored, admitted Intent Object that Trust reads through its port; Trust receives no utterance, no transcript, no model text and no conversation state.
  - **Out:** `intake.Result{Decision, Trace, DecisionID, GrantIDs, ConfirmationRequestID}`: the one decision of PERD 23.3 and the IDs the Writer returned. Nothing in it is conversational.
  - **Continuations:** `intake.ContinuationRequest` names a capability, facts, a packet reference and the disposition ID it follows: typed fields, no text.
  - **Ports:** everything outside Trust (the intent store, the registry, the pack, the evidence layer, the planner, the Recorder) is reached through the small interfaces of `ports*.go`; Trust imports none of those packages.
- Evidence it stays so: `TestTheRequestIsATypedRequestAndNotAConversation` holds the exact member set of `Request` and `Session` (a new member, above all a text member, fails it and needs this note to change); `TestNoTrustSourceImportsTheConversationSideOrAnotherPower` parses every non-test source under `services/core/trust` and refuses an import of the orchestrator, the edge, the input gateway, the understanding or evidence layers, execution, outcome, the explainer or the model service. Hand mutants, both seen failing: an `Utterance string` member on `Request`; an import of the orchestrator in `trust/clocks`. Both reverted.
- Risks, and how they are contained: the guards fix the present member set, so a legitimate new typed member is a deliberate edit of the test and of this note; that is the intent.
- Tests added (REQ-TRU-011; INV-01, INV-12): the two above. Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Signature
- EL review of the interface above: __________ (not signed by the author; the author never approves own work, AGENTS §1).

## Pre-work for the next agent
- Exact next step: EL signs or comments; the board status of TRU-12 moves only on merged evidence (the Steward).
- Do not touch: `ports.go` members of `Request` without changing the guard and this note in the same PR.
