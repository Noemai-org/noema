# orchestrator/portadapters/interpreter

The orchestrator's `Interpreter` port over the core's Interpreter (UND-04; ruling A262 on noema#565). Package `interpreterport`.

**Implements:** REQ-INT-001, REQ-INT-002, REQ-INP-002, REQ-API-001. **Never:** an identity from the text (INV-12, AGENT-10), a clock but the one it is given (INV-21), a rewrite of the person's words beyond trimming, a proposal's content or an utterance in an error text (AGENT-13).

What it does, in order:
1. **Checks the turn:** the text is trimmed and must be 1 to 2000 characters (canonical-input.v2's bound, counted in characters, not bytes); the request id is a lower-case UUID; the correlation id is `corr_…`. NFC is not done here (the model service's gateway normalises; the core has no NFC without a new dependency, AGENT-14).
2. **Resolves the session** through `Sessions` (the composition root's, from the verified session) to the Interpreter's full session and the actor kind, and refuses a resolution that is not the verified session's tenant and principal, or is not a valid session.
3. **Builds the canonical input** (`schema_version` 2.0, the turn's request and correlation ids, the session's tenant, authentication reference and principal, `text` channel, `MODE_1`, `received_at` from the injected clock in UTC) and asks the Interpreter with the pack reference.
4. **Maps the answer:** a refusal at the gate is `GATE_FAILED` with no error (the Interpreter recorded it first); an admitted intent is `ADMITTED`, or `NEEDS_CLARIFICATION` when it carries an ambiguity flag or a slot the pack requires is missing or not `RESOLVED`; the reference is `<intent_id>@<version>`. Every other failure is returned as the Interpreter gave it. An admitted node the pack does not describe is `ErrPack`, never a quiet admission.

**Deadlines:** the port passes the caller's context through and sets no deadline of its own. A deadline on every call is the Interpreter's rule (the Go standard) and the composition root's duty (`cmd/core`); `TestTheCallersContextReachesTheInterpreter` shows the context arrives unchanged.

**Admission and slots:** a required slot counts as resolved only with a `status` member that is exactly the string `RESOLVED`; any other `status` (null included) is unresolved, and a slot with no `status` is resolved only if it is a Reference (the one type that has none). The port does not rely on the gate for this.

**The reference:** `IntentRef` is `<intent_id>@<version>` (for example `int_X@1`), the form of `common.v1.json#IntentRef` (the `supersedes` link, pattern `^int_[0-9A-Za-z]{2,32}@[0-9]+$`), not the `<prefix>_<ULID>` form of a single ID. The one parser of the form is `contracts.ParseIntentRef` (`internal/contracts`, beside the generated `CommonV1IntentRef`, ruling A319), used by the Trust intake, the Trust port and the evidence adapter; its test is shared with the Record's typed-ID check (noema#661, note 1).

`RequiredFromPack` reads each node's required slots from the pack's data (`packs/fintech_v1.json`), so no slot name is a literal here (A262 point 4).

**Tests:** `cd services/core && go test ./orchestrator/portadapters/interpreter/`. They run the real Interpreter and gate with the repository's pack; the model service, the Record and the store are spies. Synthetic data.

**The G1 fixture (UND-16; A439, A460).** `g1_fixture_build_test.go` builds the canonical inputs of the five G1 inputs of the benchmark owner's file with this real port (a synthetic verified session, a fixed clock at the compile acceptance goldens' instant, an Interpreter that only keeps the request it is given) and writes them as a fixture, pinned to that file's `inputs_sha256`; the record-only pass of the compile acceptance run loads it, so the requests it records are what this port builds, with no locale hint (the locale of all five cases is en-NG). The builder is test code: nothing of it is shipped. `g1_fixture_test.go` proves it on a small SYNTHETIC inputs file of the same shape (the ten members and nothing else, the case's principal and trimmed text, stable ids, en-NG only, a closed shape and a matching digest, a pin and a byte-stable form). **Generate:** `cd services/core && go test ./orchestrator/portadapters/interpreter/ -run TestWriteG1Fixture -args -g1-inputs <inputs file> -g1-fixture <fixture file>`; without the flags it does nothing. The paths are given on the command line because the core holds no benchmark-aware string (REQ-TST-006).
