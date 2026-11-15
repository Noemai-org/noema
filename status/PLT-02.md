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

# Session 2026-11-08 12:30 WAT · PLT-02

## Session summary
- What changed: `tools/ci/install_tools.sh` curl calls now retry (5 times, all errors including HTTP 5xx, 3 s apart, 120 s cap). The SHA-256 verification is unchanged.
- Why: main was red on e208173. `Install pinned scanners` failed with `curl: (22) … 504` from GitHub's release download in `checks` and the three `images` jobs; five attempts of run 42467064026 each lost a different job to the same transient 504 or 502.
- Risks, and how they are contained: a retry cannot weaken the pin; the downloaded archive is still checked against the pinned SHA-256 before it is unpacked, so a bad body still fails.
- Tests added: shown against a local server that returns 504 twice then 200: old flags exit 22, new flags exit 0 (no repository test; shell-only change).
- Regression cases added: none (upstream availability fault; see Risks).
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: none.
- Open questions (owner): none.
- Files to read first: `tools/ci/install_tools.sh`.
- Do not touch: the pinned versions and SHA-256 values.
