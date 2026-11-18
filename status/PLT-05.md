# Status · PLT-05 · no secret in the repository; the drift workflow's checkout

Updated 2026-11-17. Plan task PLT-05 (REQ-SEC-005).

## Session 2026-11-17 · PLT-05 · hotfix: a duplicate `persist-credentials` key made `main` red

### Session summary
- What changed: `.github/workflows/docs-drift.yml`: the docs-main checkout step carried `persist-credentials: false` twice (lines 32 and 36), one from noema#622 (PLT-05) and one from noema#587 (SEC-16), merged one after the other. One line deleted.
- Why: `tools/vendor/test_require_docs_token.py` (`test_no_checkout_keeps_a_credential_in_the_workspace`) counts the `persist-credentials: false` lines against the checkouts and failed with `3 != 2`, so the `checks` job of every run on `main` was red (run 44140455492 at `f043976`). A repeated mapping key is also invalid YAML for a strict parser.
- Risks, and how they are contained: both checkouts still set `persist-credentials: false` (the test asserts it); nothing else in the workflow changes. `.github` change that reads a token: Security Reviewer and a Founder.
- Tests added: none new; the existing test is the regression case (red `3 != 2` before, green after). A scan of every workflow for repeated keys finds none.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: none. A merged-pair of pull requests that each add the same key can pass CI separately and fail together; the guard test is what caught it.
- Do not touch: the token handling of `docs-drift.yml` (SEC-16).
