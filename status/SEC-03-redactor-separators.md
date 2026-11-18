# Status · SEC-03 · the core redactor's separator set follows the Record guard (hotfix for red `main`)

Updated 2026-11-18. Plan task SEC-03 (REQ-SEC-003, REQ-OBS-002).

## Session 2026-11-18 · SEC-03 · hotfix: `main` was red after noema#457 (a semantic merge of #457 and #709)

### Session summary
- What changed: `services/core/platform/redaction/redaction.go`: the `separators` class gains `\x{1c}-\x{1f}`, the four ASCII information separators U+001C to U+001F, as the Record guard has had since noema#709 (A308: "We mask more, never less"). The doc comment says so.
- Why: noema#457 (the core redactor, merged 15:45 UTC) and noema#709 (the guard joins U+001C to U+001F, merged earlier that day) were each green alone. #457's branch last merged `main` at 10:07, before the guard changed, so its cross-check test never saw the new guard. On `main` at `f849c35` four tests in `platform/redaction/separators_test.go` fail (`TestEveryCodePointJoinsDigitsExactlyWhenTheRecordGuardSaysSo`: "U+001C between two groups of four digits is a separator for the Record guard but not for the redactor"; and three more that depend on it), so shard `tests (2)` and `core` were red (run 44327876151).
- Risks, and how they are contained: the redactor now masks a digit run that U+001C to U+001F joins; it masks more, never less. Nothing is unmasked. No other package changes.
- Tests added: none new. The four failing tests are the red proof (seen failing on `main` and locally), and they pass now. The cross-check test is the regression case: it compares every code point against the guard, so the next change to either side fails here.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: none. A259 and A308 want one shared function for the guard and the redactor; today the redactor holds a copy (platform/ imports the standard library only) and the cross-check test keeps them equal.
- Do not touch: the guard's separator set (`record/payloadguard`, a P0 path) without the same change here.
