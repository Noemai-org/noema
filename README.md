# Noema POC (noema)

Date: 2026-10-24 · Status: current · All POC code lives here (DEC-2026-10-13). Specifications live in `Noemai-org/docs`; this file defines nothing.

Read `AGENTS.md` first (a vendored copy of the docs original; the original wins). The authority order is the SoT, the PERD, the plan, then code.

## What is built

| Folder | What | README |
|---|---|---|
| `contracts/` | Versioned JSON Schemas, fixtures, golden vectors, the inventory (CON-01) | [`contracts/README.md`](contracts/README.md) |
| `packs/` | The fintech pack and three conformance packs, as data (PCK-01) | [`packs/README.md`](packs/README.md) |
| `services/core/` | Go core: skeleton and the spend-and-dispatch controller (EXE-02) | [`services/core/README.md`](services/core/README.md) |
| `services/model/` | Python model service (Session D) | see the folder |
| `benchmark/`, `synthetic/` | Benchmark and synthetic environment (Sessions B, C) | see the folders |
| `deploy/compose/` | Compose, six networks, the connection matrix (PLT-04) | [`deploy/compose/README.md`](deploy/compose/README.md) |
| `tools/` | CI checks, architecture lint, packcheck, vendored-copy tools | [`tools/ci/README.md`](tools/ci/README.md), [`tools/vendor/README.md`](tools/vendor/README.md), [`tools/packcheck/README.md`](tools/packcheck/README.md) |
| `status/` | Task status and the cross-session message log | [`status/README.md`](status/README.md) |

## Constraints

- No secret, no real customer data, no held-out benchmark material. Synthetic data only.
- Protected paths need both founders: see `.github/CODEOWNERS`.
- Contract changes follow AGENT-15. A decision goes in the PERD or the plan first; code cites the spec ID.
- No new dependency, service or tool without an [OPEN] entry and approval (AGENT-14). The Go Postgres driver and the Temporal SDK are approved (DEC-2026-10-25) and are added only in a task PR, pinned.
- `.env.example` lists names only. The two `mock-case` credential names are `MOCK_CASE_RUNTIME_TOKEN_FILE` (core) and `MOCK_CASE_BENCH_TOKEN_FILE` (benchmark runner only).
- Vendored copies of docs files (`AGENTS.md`, `CLAUDE.md` and four more) are written only by `tools/vendor/repoint_docs.py`, re-pointed in the same change set as the docs change (REQ-OPS-005).

## Open

- Review by the non-author founder on #67 (ENV-03) and docs#59 (DEC-2026-10-25).
- OD-P-70 (certificates, pack mount): default proposed, waiting for the Founder's "yes".
- Postgres driver and Temporal SDK approval for the real controller acceptance.
- `BENCH_OUT` is not wired for the per-call cost files.
- The `docs` repository has no gitleaks config; see the result on #61.
