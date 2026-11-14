
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
