# Status · PLT-02 (Session A) · CI safeguards

Updated 2026-10-24.

## State
- Merged: gitleaks (`.gitleaks.toml`, default rules on, one scoped allowlist for the public SHA-256 digests in `contracts/fixtures/golden_vectors.json`), dependency pins, environment-reference and secret-name checks, the PR trace check, `pr-base-guard` and `main-merge-audit`, with canaries (`tools/ci/run_canaries.py`).
- 24 Oct 2026: a full-history gitleaks scan of `noema` found nothing (result on noema#61).

## Open
- The `docs` repository has no `.gitleaks.toml`; its scan reports 8 findings assessed as synthetic test vectors (result on noema#61). Founders decide whether to add a scoped allowlist there.

## Messages
None sent or accepted for this task.
