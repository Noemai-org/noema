# noema_model/understanding/compiler

The model service's compile route (UND-05, slice 1; REQ-INT-001, REQ-INT-002, REQ-INT-005, REQ-API-002; INV-02, INV-12, AGENT-09). Updated 13 Nov 2026. All data is SYNTHETIC.

**What it does.** `POST /internal/v1/understanding:compile` takes `{canonical_input, pack_ref, open_intent?}` (the core's compile request) and answers `{intent_object, canonical_input}`, or an `errors.v2` envelope. `CompileService` calls a `Compiler`, holds its reply to the schema gate (`../gate`) and returns the gated proposal. The route never repairs, signs or hashes a proposal (ruling A166).

| Case | Answer | Code |
|---|---|---|
| a request that is not the contract's (wrong shape, duplicate member, canonical input not major 2, a pack this service does not hold, identity not in shape) | 422 | `INPUT_INVALID`, not retryable; no compiler is called |
| no compiler wired in this process | 503 | `DEPENDENCY_UNAVAILABLE`, retryable (the route exists; it cannot serve yet); the body is not read |
| a body over `max_request_bytes` (declared length, or counted as it streams), or a malformed `Content-Length` | 413, 422 | `INPUT_INVALID`, not retryable; a declared length over the bound is refused before one byte is read |
| the compiler found no usable provider reply, or its reply fails the schema gate | 502 | `PROVIDER_UNAVAILABLE`, retryable; the reply is never repaired |
| the compiler exceeds the configured deadline | 502 | `PROVIDER_UNAVAILABLE`, retryable |
| any other exception (a bug) | 500 | `INTERNAL_SAFETY_BLOCK`; a bug is never hidden as a provider failure |

**Identity.** The gate is held to the canonical input's own identity: `tenant_id`, `correlation_id`, `session_id` and `principal_ref`, which the core sets from the session. A proposal that carries `principal_type`, `asserted_by` or `sig` is refused by the gate. An envelope's message never carries text of the request.

**Configuration.** `CompileService(compiler, packs, max_proposal_bytes, timeout_s, max_request_bytes)`: the three numbers must be positive and come from configuration (AGENT-16); there is no default.

**The pack loader** (`packconfig.py`, slice 3; REQ-AMB-002, REQ-AMB-003, REQ-INT-005; INV-14, INV-21; AGENT-16; PR noema#582). `load_pipeline_config(pack, pack_dir, out_of_scope_node=...)` returns the pipeline's `PipelineConfig` from the loaded pack JSON: the `understanding` block (ranking weights, floor, tolerances; proposable flags; locales; material flags; the two named flags; the state-to-flag map), the candidate node's `disambiguation` (the margin and the limit of candidates stay there, where the SoT shape puts them) and the matcher file the pack names. **Nothing is defaulted.** A pack that is not whole fails at load, not at the first request: a missing, unknown, repeated or badly shaped member; an unknown ranking state; a margin outside 0 to 1; a limit that is not a positive integer; zero or two candidate nodes; a margin or limit also written in `ranking`. A `matcher_ref` that leaves the pack's folder (absolute, `..`, a symlink out) is refused. A JSON number for the margin is read as its shortest decimal (`0.35` is exactly 7/20, not the binary 0.34999...). The yes threshold of a native decision model stays unset: a benchmark and a founder decision set it (AGENT-16).

**Not here.** The compiler itself (prompt templates, the lexicon, assembly of the `intent-object.v3` proposal, the call into `providers/`) is the next slice; `create_app(compile_service=None)` is what a process without it runs. The pack resolver is a protocol: the real pack loader is PCK-01's.

**Preconditions before any PR sets `compile_service` in `main.py`** (Security Reviewer's review of noema#544; none is reachable while no compiler is wired, and the route then answers 503 and compiles nothing):
1. **Peer authentication.** Nothing enforces who may call the listener today but the network (`svc_net`, internal). mTLS on the model's listener is tracked on noema#555 and must be closed first.
2. **A concurrency cap** from configuration, and a test that `asyncio.wait_for` cancels the provider call so it cannot leak.
3. **Masking and validation at this boundary.** A masking guard (A184, `masking-limits.v1`) or the core's stated guarantee that `canonical_input` is already masked; `open_intent` is an unvalidated object passed to the compiler and must be validated; the provider-terms gate (REQ-INT-006).
4. **The echoed `canonical_input`** is the request's own: the core must never trust it as the session's.
5. **Reasons.** The gate's reasons never leave this service (a refusal is `PROVIDER_UNAVAILABLE`). If they are ever logged or recorded, the model-side reason for a model-set identity member (`SHAPE`) moves to the core's `IDENTITY_SET_BY_MODEL` (ruling A195).

**Tests.** `pytest tests/test_compile_route.py tests/test_intent_gate.py` from `services/model/`. `mypy --strict` is clean.
