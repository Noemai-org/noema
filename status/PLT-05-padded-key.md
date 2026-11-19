
# Session 2026-11-18 · PLT-05 a padded input key is a different input (Codex P2 on noema#729)

## Session summary
- What changed: `tools/ci/check_checkout_credentials.py` no longer trims the `with:` key or the value; four tests in `tools/ci/test_check_checkout_credentials.py`.
- Why: noema#729 was merged before Codex's P2 was fixed. `with: {" persist-credentials": false}` passed because the check trimmed the key; the runner does not trim input names, so the real input kept checkout's default of true and the token persisted. The same for a padded value: the action reads the boolean from the exact strings `false`, `False` and `FALSE`.
- Tests added (REQ-SEC-007): padded keys (leading, trailing, tab, both), padded values, a padded key beside the real one in either order, a non-string key. Seen failing first: 3 of the new tests fail against the merged code (41 tests; 3 failures), pass after the change. The case of the name is still ignored, as the runner does.
- Regression cases added: the four tests. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: none. The `uses` value is still trimmed (it can only make the check stricter: more steps are recognised as checkouts).
