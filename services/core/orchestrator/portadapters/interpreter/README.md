# orchestrator/portadapters/interpreter

The orchestrator's `Interpreter` port over the core's Interpreter (UND-04; ruling A262 on noema#565). Package `interpreterport`.

**Implements:** REQ-INT-001, REQ-INT-002, REQ-INP-002, REQ-API-001. **Never:** an identity from the text (INV-12, AGENT-10), a clock but the one it is given (INV-21), a rewrite of the person's words beyond trimming, a proposal's content or an utterance in an error text (AGENT-13).

What it does, in order:
1. **Checks the turn:** the text is trimmed and must be 1 to 2000 characters (canonical-input.v2's bound, counted in characters, not bytes); the request id is a lower-case UUID; the correlation id is `corr_…`. NFC is not done here (the model service's gateway normalises; the core has no NFC without a new dependency, AGENT-14).
2. **Resolves the session** through `Sessions` (the composition root's, from the verified session) to the Interpreter's full session and the actor kind, and refuses a resolution that is not the verified session's tenant and principal, or is not a valid session.
3. **Builds the canonical input** (`schema_version` 2.0, the turn's request and correlation ids, the session's tenant, authentication reference and principal, `text` channel, `MODE_1`, `received_at` from the injected clock in UTC) and asks the Interpreter with the pack reference.
4. **Maps the answer:** a refusal at the gate is `GATE_FAILED` with no error (the Interpreter recorded it first); an admitted intent is `ADMITTED`, or `NEEDS_CLARIFICATION` when it carries an ambiguity flag or a slot the pack requires is missing or not `RESOLVED`; the reference is `<intent_id>@<version>`. Every other failure is returned as the Interpreter gave it. An admitted node the pack does not describe is `ErrPack`, never a quiet admission.

`RequiredFromPack` reads each node's required slots from the pack's data (`packs/fintech_v1.json`), so no slot name is a literal here (A262 point 4).

**Tests:** `cd services/core && go test ./orchestrator/portadapters/interpreter/`. They run the real Interpreter and gate with the repository's pack; the model service, the Record and the store are spies. Synthetic data.
