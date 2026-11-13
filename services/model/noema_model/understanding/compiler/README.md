# noema_model/understanding/compiler

The model service's compile route (UND-05, slice 1; REQ-INT-001, REQ-INT-002, REQ-INT-005, REQ-API-002; INV-02, INV-12, AGENT-09). Updated 13 Nov 2026. All data is SYNTHETIC.

**What it does.** `POST /internal/v1/understanding:compile` takes `{canonical_input, pack_ref, open_intent?}` (the core's compile request) and answers `{intent_object, canonical_input}`, or an `errors.v2` envelope. `CompileService` calls a `Compiler`, holds its reply to the schema gate (`../gate`) and returns the gated proposal. The route never repairs, signs or hashes a proposal (ruling A166).

| Case | Answer | Code |
|---|---|---|
| a request that is not the contract's (wrong shape, duplicate member, canonical input not major 2, a pack this service does not hold, identity not in shape) | 422 | `INPUT_INVALID`, not retryable; no compiler is called |
| no compiler wired in this process | 503 | `DEPENDENCY_UNAVAILABLE`, retryable (the route exists; it cannot serve yet) |
| the compiler found no usable provider reply, or its reply fails the schema gate | 502 | `PROVIDER_UNAVAILABLE`, retryable; the reply is never repaired |
| the compiler exceeds the configured deadline | 502 | `PROVIDER_UNAVAILABLE`, retryable |
| any other exception (a bug) | 500 | `INTERNAL_SAFETY_BLOCK`; a bug is never hidden as a provider failure |

**Identity.** The gate is held to the canonical input's own identity: `tenant_id`, `correlation_id`, `session_id` and `principal_ref`, which the core sets from the session. A proposal that carries `principal_type`, `asserted_by` or `sig` is refused by the gate. An envelope's message never carries text of the request.

**Configuration.** `CompileService(compiler, packs, max_proposal_bytes, timeout_s)`: both numbers must be positive and come from configuration (AGENT-16); there is no default.

**Not here.** The compiler itself (prompt templates, the lexicon, assembly of the `intent-object.v3` proposal, the call into `providers/`) is the next slice; `create_app(compile_service=None)` is what a process without it runs. The pack resolver is a protocol: the real pack loader is PCK-01's.

**Tests.** `pytest tests/test_compile_route.py tests/test_intent_gate.py` from `services/model/`. `mypy --strict` is clean.
