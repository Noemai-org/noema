# evidence/recipientmatch

Date: 2026-11-26 · Owner: Core Engineer 2 (C2) · Task: EVD-04 (the Go recipient matcher) · Orders A710, A695 and OD-P-117 (interim) · All data is SYNTHETIC.

**What it does.** `Load(data)` builds a `Matcher` from the pack's recipient matcher data, and `Matcher.Match(surface, name)` is the recipient match the Candidates reader takes through its port (`evidence/candidates_api`, `RecipientMatcher`): an exact share `{numerator, denominator}` in [0, 1], in lowest terms, computed in core from the request's `counterparty_surface` and the counterparty's name. Only the share leaves (A676). `Score(query, name, phonetic)` is the same with the voice path's tier switchable.

**REQs.** REQ-AMB-003 (the composed recipient matcher), REQ-CTX-006. **SoT.** INV-01 (no model), INV-14 (no name knowledge in code), INV-21 (exact, replayable), AGENT-13 (no name in an error), AGENT-16 (no default for a number).

**Parity is the acceptance.** It reproduces the Python reference matcher (`services/model/noema_model/understanding/resolution/matcher.py`) exactly: NFC, case folding, NFKD, nonspacing marks dropped, the pack's folding map; tokens are letter and mark runs with apostrophes kept, at most eight; each pair of tokens scores its best tier (exact, folded, alias, phonetic, initial); the total is the best one-to-one assignment (a query token may stay unmatched) divided by the number of query tokens, in exact fractions (`math/big`). `contracts/fixtures/recipient_match_vectors.json` holds 644 SYNTHETIC cases, built from the shipped data and from a second inline dataset whose five tier scores are all different; the Python test (`services/model/tests/test_matcher_vectors.py`) and `matcher_test.go` both load it and must give the same fraction for every case. The expected values come from the Python reference (the authority).

**Data.** The matcher's whole name knowledge is `packs/fintech/lexicons/recipient_matcher.json`: tiers, folding map, nickname and spelling-variant tables, phonetic rules. It is pack data under the manifest pin (kind `recipient_matcher`, required from manifest version 5; `platform/packload` returns the verified bytes as `Pack.RecipientMatcher`, and the matcher is built from those bytes, never from a second read). The loader refuses a file that is not exactly that shape (`ErrData`, fixed text): an unmarked entry, an unknown member, a tier that is a float, zero, above one or rises from exact to initial, a bad folding key, a variant of one spelling, a phonetic rule from nothing.

**Not here.**
- **The phonetic tier through the port.** The Candidates request has no `phonetic` member, so the port scores with the tier off: for that tier it reports no match. `Score` implements the tier and the vectors test it on and off. Switching it on from a request is a contract question for the Architect (a new optional member of `candidates-request`).
- **Wiring.** The composition builds the reader and passes this matcher (EL's). No v1 consumer is touched.
- A share whose numerator or denominator does not fit an `int64` is `ErrShare` (fixed text): the shipped tiers are small; the bound is for other data.

**Tests.** `go test ./evidence/recipientmatch/` from `services/core/`. Hand mutants cover each tier, each folding step, the token cut, the assignment, the phonetic key and the loader's refusals.
