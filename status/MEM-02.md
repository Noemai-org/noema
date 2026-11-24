
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

# Session 2026-11-24 · MEM-02 · the linked regression case must exist (A652)

## Session summary
- What changed: `tools/ci/check_regression_link.py` (a case-ID link must name a case that exists in the generated suite of the checkout the check runs in; `known_case_ids` is a required keyword; an unreadable suite fails closed), `.github/workflows/regression-link.yml` (one step: the benchmark's hash-checked requirements, before the check), `tools/ci/test_check_regression_link.py`, `tools/ci/test_regression_link_workflow.py`, `benchmark/generator/test_known_case_ids.py`.
- Why: A652 (both Founders) asks for a CI check that a linked regression case exists or is added in the PR. REQ-MEM-002 says "a linked new case ID", and until now the check saw only an ID's shape: `BEN-AMB-9999` passed. The workflow checks out the PR's merge commit, so a case the PR adds is in the suite it builds, and a case that already exists is too: both satisfy "exists or is added".
- What it does not decide: it does not prove the case is NEW (an old case still passes), nor permanent, nor the order of commits; the reviewer reads those (AGENTS §8). The A156 test-file alternative is unchanged and needs no suite. The `REGRESSION_LINK` observation (REQ-MEM-002's first half) has no writer in core yet; that is EL's, and MEM-02 is not Done until it is decided.
- Risks, and how they are contained: a workflow change (`.github`: SR's read and both Founders' approval, as A652 says). The step installs only the hash-checked lock the benchmark job already uses; the workflow keeps `contents: read` and `pull-requests: read`, no secret, no `pull_request_target`, no interpolation of untrusted text. The check now builds the suite (about 7 seconds), only for a fix PR that links a case ID.
- Tests added (REQ-MEM-002, A652): 10 in `test_check_regression_link.py` (exists, unknown and named, every link, unreadable fails closed, required parameter, non-fix and test-file paths never read the suite, the command loads and refuses), 1 in the workflow test (the install, hash-checked, before the check, the only install), 2 against the real suite. Seen failing first (28 errors).
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: SR reads the workflow step; both Founders approve (A652). EL decides the `REGRESSION_LINK` observation writer.
- Do not touch: the A156 alternative's behaviour.
