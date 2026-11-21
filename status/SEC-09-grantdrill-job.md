# Session 2026-11-20 · SEC-09 · the core image carries the operator commands; the rotation drill as a CI job (Integration Lead)

## Session summary
- What changed: `services/core/Dockerfile` builds `cmd/core`, `cmd/grantdrill` and `cmd/policypublish` into `/usr/local/bin/` (the entrypoint stays the core); a `grantdrill` job in `.github/workflows/compose.yml` builds the image and runs the rotation drill in it with a tmpfs work directory (no network, read-only, all capabilities dropped, no secret, no mount); `deploy/compose/test_grantdrill_job.py` (run in `compose-scripts`) reads the real Dockerfile and workflow.
- Why: SR's answer on noema#692 (the drill needs no stack: it runs in the core image) and A445 (the policy-publish one-shot runs `policypublish` from the core image with its own entrypoint).
- Risks, and how they are contained: two more binaries in the core image. `grantdrill` makes synthetic keys and prints counts; `policypublish` takes a DSN file by path; neither is an entrypoint and the core service in Compose does not run them. Pinned bases, static build and the nonroot user are unchanged and tested.
- Tests added (REQ-SEC-005, INV-03, INV-25, INV-29): five guards; failed first (no commands in the image, no job).
- Not verified: the image build and the drill run (no Docker daemon in my session): CI is the proof. The part only the stack can show (the core container loading the two mounted keys) still waits for the core's `NOEMA_GRANT_KEY_DIR` wiring (EL).

## Pre-work for the next agent
- Exact next step: after the core reads `NOEMA_GRANT_KEY_DIR`, add the stack-level proof of noema#692 (the core container loads the two keys; a drill restart with two active keys; `KEY_ROTATED` in the Record).
- Do not touch: the image's entrypoint.
