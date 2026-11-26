# Status · OPS-47 · main red: the wiring tests and the Writes input after the merge of #990

## State
- Cause: #990 (STM-01, Writes built, `a8beaa6`) and #956 (the wiring tests, `456fedb`) were each green on their own head. Merged together, four tests of `compose_trust_wiring_test.go` still expected Writes to be unbuilt, and `buildWritesInput` returned an error instead of a gap when the Trust read timeout was absent (`TestAnAbsentInputIsANamedGapAndNotAnError/no_read_timeout`: "a gap is not an error").
- Fix: Writes is expected built where the pack, tenants and logins are given (the four expectations drop "Writes" from the unbuilt lists; the production test expects Writes built); an absent timeout is a gap in `buildWritesInput`, as for the other inputs (a given invalid bound is refused where the setting is read, unchanged).

## Messages
None.

# Session 2026-11-26 · OPS-47

## Session summary
- What changed (PR links): see the pull request that carries this file.
- Why: main red on `core` and `tests (0)` at `2b79faf` since `a8beaa6`.
- Risks, and how they are contained: one condition in `compose_writes.go` (a gap, never a default: fails closed) and test expectations; `go vet` and `gofmt` clean; the local `cmd/core` failures equal main's sandbox baseline, none new.
- Tests added (REQ, INV, US or EC IDs): none new; the four tests above are corrected (REQ-STM-001).
- Regression cases added: the corrected tests.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: SR reads the `compose_writes.go` condition (Tier 1).
- Open questions (owner): `buildWritesInput` returns a gap for a pack with no `write_when`, while its comment and A693 say that is "no write", not a gap (EL).
- Do not touch: the early-decision form of A663.
