
# Session 2026-11-12 15:00 WAT · DEBT-16

## Session summary
- What changed (PR links): `.github/workflows/compose-scripts.yml` runs `test_init.sh` and `test_provision_env.sh` in the PostgreSQL 17.11 + pgsodium image built from `deploy/spike/pgsodium` (as `core.yml` does), not in the stock `postgres:17.11-alpine`. Hotfix for a red `main` (A129).
- Why: `main` was red at `e4798a09` (merge of noema#257): `compose-scripts` / `test_init.sh` failed with `Could not open extension control file ".../pgsodium.control"`. `init.sh` has created the `pgsodium` extension since EXE-08 (D10a, noema#229), and the stock image does not have it. noema#257's own checks were green on its head, before that dependency reached `main`.
- Risks, and how they are contained: workflow only; it runs the same two scripts against the same Postgres major and base image plus the pinned pgsodium build. No secret, no new action, no new dependency (the Dockerfile is already pinned and used by `core.yml`).
- Tests added (REQ, INV, US or EC IDs): none new; the `compose-scripts` job is the check.
- Regression cases added: the job itself.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: none. SR reads the workflow diff (`.github`, A143); Codex after the merge (A129).
- Open questions (owner): whether a stale-green merge like this should be caught by a merge-result check (Founders).
- Files to read first: .github/workflows/compose-scripts.yml, deploy/spike/pgsodium/Dockerfile.
- Do not touch: the pinned digests.
