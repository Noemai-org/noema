
# Session 2026-11-17 · MEM-02 · the regression-link check is wired into CI

## Session summary
- What changed: `.github/workflows/regression-link.yml` (new), one step in `.github/workflows/safeguards.yml`, `tools/ci/test_regression_link_workflow.py` (new).
- Why: the check merged with noema#452 ran nowhere ("Not wired into a workflow here: `.github` is not on this task's path"). OD-P-83 (docs#160, merged) says the entry stays open until the workflow wiring PR; ruling A156 (docs#157) is committed, so the test-file alternative is turned on.
- Risks, and how they are contained: a workflow change (Founder approval and Security Reviewer). The workflow holds `contents: read` and `pull-requests: read` only, no secret, no `pull_request_target`; it reads the live pull request through the API and puts no untrusted text in a script (only the pull request number is an expression); the checkout keeps no credential; every action is pinned to a commit. It re-runs on a label added or removed and on a description edit. The 27 script tests, which ran nowhere, now run on every pull request.
- Tests added (REQ-MEM-002): `test_regression_link_workflow.py` (13): shape, events, concurrency convention, least privilege, pinning, live data, no interpolation, A156 on, no `if:`, names agree with the script, safeguards runs the tests, and the script run as the workflow runs it (a non-fix passes; a `fix` label without a link fails; a case id passes; a changed test file passes only because the alternative is on; a test file the PR does not change fails). Seven mutations of the workflow, each caught.
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: after this merges, the `docs` entry OD-P-83 is closed by the Repo Steward (it says it stays open until this wiring PR).
- Open questions (owner): whether the check should be a required status check once branch protection is enabled (the Founders).
- Files to read first: `.github/workflows/regression-link.yml`, `tools/ci/check_regression_link.py`.
- Do not touch: nothing outside these files.
