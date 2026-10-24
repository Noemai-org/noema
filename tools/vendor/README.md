# tools/vendor

Keeps the vendored copies of docs files in step with their originals in `Noemai-org/docs` (GOV-22; PERD REQ-OPS-005).

## What is built

- `repoint_docs.py`: rewrites each of the five copies (`AGENTS.md`, `CLAUDE.md`, `contracts/state-registry.md`, `contracts/perd-32-2-event-types.md`, `services/core/orchestrator/perd-28-2-state-machine.md`) from a clean docs checkout, header and body.
  - `--docs <checkout>`: re-point.
  - `--check`: report differences, change nothing.
  - `--check --ignore-commit`: a copy that differs only in the docs commit named in its header is not a difference (the drift guard uses this).
- `require_docs_token.py`: fails with one clear line when `DOCS_READ_TOKEN` is missing.
- Tests: `test_repoint_docs.py`, `test_repoint_bodies.py`, `test_drift_guard.py`, `test_require_docs_token.py` (`python3 -m unittest discover -s tools/vendor`).
- The daily `docs-drift` workflow runs the check against docs `main` with a read-only token.

## Constraints

- Never hand-edit a vendored copy; run the script. The docs original wins.
- The script reads docs only; the docs checkout must be clean.
- A docs change touching vendored text needs the matching re-point in the same change set (REQ-OPS-005).

## Open

- OD-P-68: opening the re-point pull request automatically (proposal, not built).
