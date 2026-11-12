# Status · UND-04 (Core Engineer 2) · the core's side of the schema gate and the intent client

Updated 2026-11-11. Plan task UND-04: REQ-INT-001, REQ-INP-002; allowlist `services/core/understanding/**`. The Python half (`services/model`, the compile endpoint) is separate.

## State: in review (PR on `feat/UND-04-core-schema-gate`)

Built: `services/core/understanding` (gate, slot rules, identity overwrite, intent hash, compile client). Constructors only: EL wires them into `cmd/core/compose.go`.

## Open
- CONFLICT: the intent hash over a fractional number (see the PR). Fails closed meanwhile.
- The session fields `authn_ref`, `principal_type`, `asserted_by` are not on the edge's session yet.
- UND-02's core side (the handler for `POST /v1/conversations`): the Input Gateway already exists in `services/model` (435f9c37, fdc47a6f), and the dedupe of REQ-INP-003 needs a table that no migration has. Asked on a `[WAITING: C2 needs Architect]` issue before any code.

## Messages
| Date (UTC) | From | To | Purpose | What the receiver used it for |
|---|---|---|---|---|
| 2026-11-11 | Architect (A146/A147) | C2 | Start UND-02 and UND-04 as constructors EL wires into compose.go (allowlist `inputgateway/**`, `understanding/**`) | This change (UND-04); UND-02 waits on the question above |

# Session 2026-11-11 16:30 WAT · UND-04

## Session summary
- What changed (PR links): the UND-04 core PR on `feat/UND-04-core-schema-gate`.
- Why: REQ-INT-001 (the second schema gate), REQ-INP-002 (identity from the session), the compile client of PERD §39.3.
- Risks, and how they are contained: no Go JSON Schema validator (AGENT-14), so the closed shape is the generated decode and the rest is explicit rules, each with a test; the hash cannot cover fractions, so the gate refuses them (HASH_UNAVAILABLE) rather than hash by another rule.
- Tests added (REQ-INT-001, REQ-INP-002, INV-02, INV-12): `understanding/gate_test.go`, `understanding/client_test.go`.
- Regression cases added: none (new package).
- Debt entries: none.
- Conflicts raised: intent hash over JCS versus fractional confidences.

## Pre-work for the next agent
- Exact next step: decide the hash form; then the edge maps the session fields and wires `Gate` and `Client` into `compose.go`; the caller records PRINCIPAL_OVERRIDE_ATTEMPT from `Admission.Overrides`.
- Open questions (owner): hash form (Architect); UND-02 core scope (Architect).
- Files to read first: `services/core/understanding/README.md`, `gate.go`.
- Do not touch: `platform/crypto` (not on this allowlist).

# Session 2026-11-12 (night) · UND-04 core · the gate follows A166

- **What changed:** (1) `gate.go`: `sig` must be exactly the neutral marker (`unsigned`/`unsigned`) the model service's own gate writes (noema#489), so a signature the model made is `SIG`; (2) `principal_type` and `asserted_by` must be exactly the marker `unset`, otherwise `SHAPE` (the model sends neither, A166 item 3), and `overwrite` no longer reports them, so a normal compile returns no overrides and the caller records no `PRINCIPAL_OVERRIDE_ATTEMPT`; (3) `packview.go`: `NewPackView` reads the gate's `PackView` from `packs/fintech_v1.json` (A166 item 4). The pack has its nodes (`fin.payments.exception.investigate`, `fin.meta.out_of_scope`), so no [OPEN] entry is needed.
- **Failing first:** `a166_test.go` (3 tests: 7 + 5 refusals) and the changed fixtures were red locally (14 failures); `packview_test.go` did not compile before `NewPackView`. The red state is local only, in one push.
- **Tests added (REQ-INT-001, REQ-INP-002; INV-02, INV-12, INV-29):** `a166_test.go`, `packview_test.go` (the view from the real pack; the gate over it, admitting the pack's node and refusing a slot, outcome, node or slot type it does not define; 20 refused packs; the view does not share the caller's bytes). `gate_test.go`: the fixture now carries the markers; the identity test varies only tenant, principal id and authn ref.
- **Mutations by hand, each red:** the old sig rule; `&&` for `||` in the sig rule; each of the two identity markers unchecked; both unchecked; `principal_type` reported as an override again; and on the view: node of another pack, undefined outcome, repeated slot, unknown slot type, repeated outcome, repeated node, repeated outcome definition, no nodes, pack version shape, pack id shape (the last two survived until a case with the nodes agreeing was added), and the copy in `Node` replaced by sharing.
- **What this does not settle (stated):** the signature itself. A166 item 1 says the core computes the hash and the signature after the gate with the REC-08 emitter signer. The admitted object keeps the marker in `sig`, and the hash covers it, so the stored document stays equal to what the hash covers (noema#471's `verify`). My reading, to be confirmed by the Architect on noema#484: the core's signature is the signed `INTENT_ADMITTED` Record event (it carries `intent_hash` and the emitter signer signs it), not a member of the document. If a separate signature over the intent is meant, it needs a body shape and a domain tag, which the emitter signer does not have today (`SignRecordEvent` signs Record event bodies).
- **Next (not here):** the `Interpreter` (compile, gate, record `INTENT_REJECTED_AT_GATE` or the override, save through noema#471's store) after #471 merges, and the model-service route after noema#489 merges.
- Debt: none. Conflicts raised: none.
