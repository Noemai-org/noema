# understanding

**What it does.** The core's side of the Understanding boundary (UND-04, core half): the second schema gate on an Intent Object that the model service returned, the overwrite of the principal and tenant from the session, the intent hash, and the client of `POST /internal/v1/understanding:compile`.

**REQs.** REQ-INT-001, REQ-INP-002, REQ-INT-002 (the client side). SoT INV-02, INV-12, AGENT-09, AGENT-10. Boundary 4 of PERD §18.4.

**Built.**
- `gate.go`: `Gate.Admit(raw, session, correlationID)` returns an `Admission` or a `*Rejection`. The raw bytes must be one JSON object with the closed shape of `intent-object.v1` (generated decode: unknown, repeated, missing and null members are refused). Then the rules a closed shape cannot say: status `VALIDATED`, schema version, intent id and version, the supersedes chain, `created_at`, the correlation id equal to the edge's, mode 1 only, the pack id and version equal to the loaded pack, the node known to the pack, every slot named by the node and of the node's type, `desired_outcome_refs` a subset of the node's outcomes, a signature that is exactly the neutral marker (the model service never signs, ruling A166 item 1), `principal_type` and `asserted_by` that are exactly the marker `unset` (the model sends neither, item 3). All reasons are collected, sorted and unique. A rejection names reasons and never quotes the object.
- `packview.go`: `NewPackView(raw)` reads the gate's `PackView` from the mounted pack data (`packs/fintech_v1.json`): pack id and version, each intent node's slots by name and type, each node's outcomes. It refuses a repeated node, slot or outcome, a node of another pack or version, a slot type the gate does not know and an outcome the pack does not define (`ErrPack`). Nothing is hard-coded in the core (ruling A166 item 4).
- `slots.go`: the per-type slot rules (a RESOLVED slot needs a well-formed `resolved_ref`, INV-06; ratios in [0,1]; a quantity is a non-negative safe integer with a currency or pack unit).
- `identity.go`: `overwrite` writes the session's tenant and principal fields over the object and returns the NAMES of the identity members that differed (`tenant_id`, `principal_id`, `authn_ref`; `principal_type` and `asserted_by` are always the session's and are never an override, so a normal compile reports none) (the caller records `SECURITY_EVENT PRINCIPAL_OVERRIDE_ATTEMPT` with them; never the values). `canonical` and `hashOf` give the JCS form and `sha256:<hex>` of the object **after** the overwrite.
- `client.go`: `Client.Compile` (deadline required, strict decode of `{intent_object, canonical_input}`, bounded read, redirects not followed, the errors.v2 envelope mapped to `PROVIDER_UNAVAILABLE`, `INPUT_INVALID`, `UNAVAILABLE` or `MALFORMED`). It holds no credential: the caller builds the mTLS `http.Client`.

**An `AdmittedIntent` is the only thing Trust may receive.** It has no exported constructor except `Gate.Admit`.

**Open (see the PR's CONFLICT block).**
- The intent hash cannot cover a fractional number: `platform/crypto.CanonicalJSON` refuses one and the Intent Object carries fractional `confidence` and `asr_confidence`. The gate therefore refuses such an object with `HASH_UNAVAILABLE` (the safer reading) until the form of the hash is decided.
- No JSON Schema validator exists in Go (AGENT-14), so the gate is the generated closed-shape decode plus the explicit rules above. It does not model the patterns of the schema that the rules above do not restate; `tools/contracts` validates the same fixtures in Python.
- `Session` takes `authn_ref`, `principal_type` and `asserted_by` as arguments. The edge's `session.Principal` does not carry them yet; whoever wires the edge maps them.

**Tests.** `go test -race ./understanding/...` from `services/core/`. The 10,000-case unknown-field run uses a fixed seed. Mutation checks done by hand: no overwrite, no correlation check, no outcome-subset check and no Mode 2 refusal each fail the suite.
