# Status · PLT-02 (Session A) · CI safeguards

Updated 2026-10-24.

## State
- Merged: gitleaks (`.gitleaks.toml`, default rules on, one scoped allowlist for the public SHA-256 digests in `contracts/fixtures/golden_vectors.json`), dependency pins, environment-reference and secret-name checks, the PR trace check, `pr-base-guard` and `main-merge-audit`, with canaries (`tools/ci/run_canaries.py`).
- 24 Oct 2026: a full-history gitleaks scan of `noema` found nothing (result on noema#61).

## Change of 26 Oct 2026 (Founder-approved; this PR)

- The safeguards secret scan no longer scans every remote ref. `tools/ci/gitleaks_range.py` scans only the commits of the change: a pull request's `base.sha..head.sha`, or `before..sha` for a push; with no usable base it scans the head's own ancestry, never other refs. The range is printed in the log. `test_gitleaks_range.py` (6 tests, real pinned gitleaks) proves a secret on another branch is not scanned, a secret in the range is found, and gitleaks's own default would have scanned the other branch.
- Why: the abandoned branch `sa/env-03-case-adapter` (one placeholder finding, never in a PR) made `checks` fail on every pull request.
- Not changed: no rule disabled, no allowlist entry, no path excluded. A scheduled full-history scan of `main` is not part of this change.

## Open
- The `docs` repository has no `.gitleaks.toml`; its scan reports 8 findings assessed as synthetic test vectors (result on noema#61). Founders decide whether to add a scoped allowlist there.

## Messages
None sent or accepted for this task.
