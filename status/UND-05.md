
# Session 2026-11-10 · UND-05 · a hard stop for the live run (DRAFT; the USD 5 figure is not yet in docs/decisions)

## Session summary
- What changed: `benchmark/bakeoff/live_run.py` gets `--hard-stop-micros` (optional, positive integer; the guard's limit is the smaller of the phase cap and the hard stop, so it can only tighten; prior spending counts). `.github/workflows/bench-live.yml` gets a required `hard_stop_usd` input (default 5), validated as a positive whole number in the Run step and converted to micro-dollars. The workflow still reads only `OPENAI_API_KEY`; no other secret was added.
- Why: Architect sync of 10 Nov 2026: one OpenAI-only synthetic run with a USD 5 hard stop (A79 under the USD 25 cap is committed; the USD 5 figure is the sync's and is not yet in docs/decisions). Security Reviewer reviews the workflow change before any value is added (noema#338).
- Risks, and how they are contained: workflow_dispatch only; `environment: bench-live` (required reviewer); the key reaches one step; the hard stop cannot raise the phase cap; the phase caps ($2, $25) in `gates.py` are untouched. A live run needs the Founder to place `OPENAI_API_KEY` in `bench-live` and confirm the name on noema#338; nothing was run.
- Tests added (REQ-BEN-005, REQ-NFR-007, REQ-AGT-005): `HardStop` in `benchmark/bakeoff/test_live_run.py` (red first, 520c041): below-cap hard stop is the limit; above-cap cannot loosen (full and pilot); none leaves the cap; prior spending counts; non-positive and non-integer refused.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: Security Reviewer reads the workflow diff (noema#338); the Founder confirms the key name; no live call before both.
- Files to read first: `.github/workflows/bench-live.yml`, `benchmark/bakeoff/live_run.py`.
- Do not touch: `benchmark/bakeoff/gates.py` caps.

# Session 2026-11-14 · UND-05 core side of mTLS (noema#555; the Founder's order of 14 Nov; ruling A208, relayed, not yet committed)

## Session summary
- What changed (this PR): `cmd/core/modelclient.go`: `buildModelHTTPClient` and `buildModelClient` build the core's client of the model service over **mutual TLS**: the core presents its own certificate (`NOEMA_MODEL_CLIENT_CERT_FILE`, `NOEMA_MODEL_CLIENT_KEY_FILE`), trusts only the CA file it is given (`NOEMA_MODEL_CA_FILE`, not the system's roots), refuses a URL that is not `https://` (`NOEMA_MODEL_URL`) and takes the response bound from `NOEMA_MODEL_MAX_RESPONSE_BYTES` (no default). All five or none; with none the Interpreter stays unconfigured and `/readyz` says so. TLS 1.2 minimum, as the egress gateway's. No error carries a key.
- Why: noema#555 (the Security Reviewer's blocking finding on noema#544: the model service's listener checks nobody). A208 accepts IL's design: uvicorn terminates mTLS, IL does the Dockerfile and Compose, EL does this core client.
- Risks, and how they are contained: this is the client half only; until IL's half lands (uvicorn with a CA and `CERT_REQUIRED`) the listener still checks nobody, and noema#544 must not wire a compiler (rule 5 of noema#555). Mutants run locally, four killed (server certificate not verified; no client certificate presented; plain http accepted; a bound of zero accepted).
- Tests added (INV-29, AGENT-13, AGENT-16; PERD 36.1): `cmd/core/modelclient_test.go`: a real mutual-TLS server (certificates made at run time with the standard library): the core's certificate is what the server sees; a server certificate from another CA is refused; a core certificate from another CA is refused by the server; eleven bad configurations refuse to build with no key echoed; the understanding client is built over it.
- Not done: plugging the client into `Ports.Interpreter` (the interpreter adapter and `main.go`: the composition PR), the model service's side (IL), the certificates' provisioning (OQ-09: file-mounted until decided).
- Debt: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: IL adds uvicorn's `--ssl-*` options and the Compose mounts; EL plugs `buildModelClient` into the composition and sets `NOEMA_MODEL_*` in the stack.
- Files to read first: `cmd/core/modelclient.go`, `understanding/client.go`.
- Do not touch: the model service's listener (IL), `understanding/client.go`.

# Session 2026-11-13 21:30 WAT · UND-05 · the model service's compile route (slice 1, noema#544)

## Session summary
- What changed (PR noema#544): `POST /internal/v1/understanding:compile` and `CompileService` in `services/model/noema_model/understanding/compiler/` (route, service, README); `create_app(compile_service=None)` mounts it (no service: 503 `DEPENDENCY_UNAVAILABLE`, never 404); the model-side schema gate moves to `intent-object.v3` / `intent-slot.v2` (ruling A173: confidence as integer basis points, `schema_version` `3.0`).
- Why: the core's client calls this route and the model service had none (G1 needs US-01 through the public API, noema#436). Ruling A188 asks for the compiler against `intent-object.v3`.
- Risks, and how they are contained: the route never repairs, signs or hashes a proposal (A166); a reply the gate refuses is a `PROVIDER_UNAVAILABLE` (502, retryable) with no reason in the answer (AGENT-13); a bug is a 500 `INTERNAL_SAFETY_BLOCK`, never hidden as a provider failure; size bound and deadline come from configuration, no default (AGENT-16). The core's gate still decodes v1: its move to v3 is a separate change of mine.
- Tests added (REQ-INT-001, REQ-INT-002, REQ-API-002; INV-02, INV-12, AGENT-09): `tests/test_compile_route.py` (written first; red run 43506966574, 12 failed and 1090 passed), `tests/test_intent_gate.py` moved to v3 (non-integer and out-of-range confidence is a shape refusal; schema version 3.1 refused).
- Regression cases added: duplicate-member request is refused as `INPUT_INVALID` (it first produced a 500); a canonical input of major 3 is refused.
- Debt entries: none. Conflicts raised: none. The red commit's docstring named a core path and failed `py-cross-service` (arch-lint); the implementation commit rewords it.

## Pre-work for the next agent
- Exact next step: the compiler itself (prompt templates, lexicon, assembly of the `intent-object.v3` proposal, the `providers/` call) behind the `Compiler` protocol; wire a `CompileService` in `main` once a real pack loader exists (PCK-01); then the core's gate and proposal move to v3.
- Open questions (owner): `principal_ref` in the canonical input must carry the session's `usr_` id (C1, input gateway); noema#528 (recorded-replay provider) and noema#546 (compile acceptance) for the live run.
- Files to read first: `services/model/noema_model/understanding/compiler/README.md`, `.../gate/README.md`.
- Do not touch: the generated contracts; the gate's reason set.

# Session 2026-11-14 11:00 WAT · UND-05 · the Security Reviewer's NOT READY on noema#544

- **Finding 2 (unbounded request body): fixed.** `CompileService` takes `max_request_bytes` (required, positive, no default). The route answers 503 before reading anything when no service is wired; otherwise it refuses a declared length over the bound without reading a byte (413, `INPUT_INVALID`), cuts a body with no declared length as it streams, and refuses a malformed `Content-Length`. Failing first: `tests/test_compile_route.py` at `3b0b2ffa` (the `model` job failed, run 43586455374). Six mutants of the new code were run: five killed by the route tests; one (the declared-length check ignored) survived until an ASGI-level test that the receive channel is never touched was added, and is now killed.
- **Finding 1 (mTLS on the model listener): not fixable in this PR.** It needs `services/model/Dockerfile` and `deploy/compose` (IL's, protected). Tracked on noema#555; stated in `compiler/README.md` as the first precondition before any PR sets `compile_service` in `main.py`. The route compiles nothing until then.
- **Notes 3 to 6:** recorded as preconditions in `compiler/README.md`; none is reachable while no compiler is wired.
- Debt: none. Conflicts raised: none.

### Update 2026-11-14 · Security Reviewer's NOT READY on noema#563 (EL)
- **Blocking (the setting names did not match noema#568).** The core now reads IL's names: `NOEMA_MODEL_TLS_CA_FILE`, `NOEMA_MODEL_TLS_CERT_FILE`, `NOEMA_MODEL_TLS_KEY_FILE` (the constants keep their Go names). `NOEMA_MODEL_URL` is unchanged. **`NOEMA_MODEL_MAX_RESPONSE_BYTES` is not in noema#568's Compose file and the core refuses to build the client without it (no default, AGENT-16): IL adds it to the core service there, or the all-or-none rule keeps the Interpreter off.** The test that reads the Compose file's names belongs with the Compose file (noema#568 owns `deploy/compose`; main has none of these names yet), so it lands with #568 or after it merges; asked of IL on #568.
- Merge conflict with main resolved (a merge; `status/UND-05.md` kept both sections).
- Note (c): `TestAModelServiceWhoseCertificateNamesAnotherHostIsRefused` (a certificate from the trusted CA for 127.0.0.2, URL 127.0.0.1). Mutant killed: `InsecureSkipVerify: true` fails it and the other-CA test. Notes (a) and (b) (no rotation without a restart; no timeouts of its own, the caller's deadline bounds each call) are as the reviewer reads them. Dependency (noema#555): `buildModelClient` is not called from `main.go`; the composition PR calls it at startup and exits on error.
