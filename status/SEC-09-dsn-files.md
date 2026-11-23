# SEC-09-dsn-files

Date: 2026-11-22 · Owner: Core Engineer (helper session for EL) · Status: red pushed, green held locally

Task: SEC-09 (REQ-SEC-009, INV-29, AGENT-13). Founder instruction A509 and issue #810, relayed, not yet committed: every secret file `cmd/core` reads goes through the shared checked reader `keyfiles.ReadFile` (regular file, no link, no FIFO, owner is the running user, mode 0400, bounded).

## State

- Sites moved to `keyfiles.ReadFile`: the per-tenant DSN files of all four DSN directories (`loginPools`: `NOEMA_RECORD_READ_DSN_DIR`, `NOEMA_ORCHESTRATOR_DSN_DIR`, `NOEMA_TRUST_DSN_DIR`, `NOEMA_EDGE_DSN_DIR`), the model-service client private key (`NOEMA_MODEL_TLS_KEY_FILE`) and the Temporal client private key (`NOEMA_TEMPORAL_TLS_KEY_FILE`).
- Not moved (public material or outside `cmd/core`): certificates and CA files (public); the session public key and the limits file read by `edge.Load` (public key PEM and non-secret limits; `edge/` is outside the lane).
- Bounds are constants, not settings: `dsnFileMaxBytes` 4096, `privateKeyMaxBytes` 16 KiB.

## Session summary (2026-11-22 WAT) 
- What changed: see the PR.
- Tests added: `cmd/core/dsnfiles_checked_test.go` (REQ-SEC-009, INV-29, AGENT-13). Secret fixtures in six existing test files now use mode 0400.
- Regression cases added: `TestADSNFileThatIsNotOwnerReadOnlyIsRefused` and its siblings.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: push the held green commit after reading the red job.
- Open questions (EL): should `edge.Load` read its session public key and limits through a checked reader too (public data; a file in `edge/` is outside this lane)? Mounted DSN files must now be 0400, owned by the core's user: the deploy step must chmod them (no `deploy/` change was made here).
- Do not touch: `cmd/migrate` (EXE-08).
