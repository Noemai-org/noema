# Status · BEN-14 (Benchmark Owner agent) · hallucination corpus for the claim validator

Updated 2026-11-03. Owner: B. Needs: none. REQ-EXP-002 (B5).

## State

- **Built:** `benchmark/hallucination/`: a generator (seed 20261103), `corpus.v1.json` (250 items: 200 bad in eight classes, 50 good controls), a schema, a reference checker (`verify.py`) and a CLI. Tests first (seen failing), 17 tests. Done-when ("at least 100 bad explanations in the corpus") met twice over; REQ-EXP-002's 200 is met.
- **Not the validator.** The Go validator in core is another role's (Core Engineer 2). This corpus is its test data.

## Open

1. **yo, ha, ig.** Native-speaker review of the forbidden-claim lists is a REQ-EXP-002 dependency; the corpus covers en-NG and pcm only and says so.
2. **Wiring into the validator's test** belongs to the EXP task owner; the README says how.

## Messages

### 2026-11-03 (time not recorded) accepted · Founder (standing orders, relayed by the user) → Benchmark Owner agent
- Purpose: start independent tasks the moment they can start; no scope cut.
- Receiver used it for: BEN-14, which has no prerequisite.
- Evidence: the PR for this branch.
