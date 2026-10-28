# synthetic/proposer_swap

SYNTHETIC. Files shared by the two halves of the proposer swap test (`status/ENV-08.md`, 28 Oct 2026). **This validates the decision path only. It does not validate the unbuilt intake or the complete architecture.**

**What it holds.**
- `proposals/<scenario>.<A|B>.json`: what two proposers hand over for the same canned decision. A is a canned-proposal stub; B is the OpenAI Responses adapter replaying a canned provider response. Neither made a network call. Each file has the provenance (`proposer`: provider, model version, tokens), the pipeline outcome and the Intent Object (SoT §43.1, PERD §20.1, `contracts/schemas/intent-object.v1.json`), or `null` where the schema gate rejected it. Scenarios: `escalate` (valid), `prohibited` (a refund remedy, PROHIBITED), `invalid` (answer the gate rejects).
- `decisions.json`: the decision the real `trust/decide` constructors (`Admit`, `FailAtGate`) make for each scenario, identical under both proposers.

**How the halves meet.** The core cannot import across `trust` and `execution`, so: the model service's tests write the proposals; `services/core/trust/decide/proposer_swap_test.go` reads them, decides, and fails if `decisions.json` is stale; `services/core/execution/e2e/proposer_swap_test.go` reads both and runs the real `EscalateCase` workflow on mock-case, comparing outcomes and Record events. Each side fails if its committed input is stale.

**Regenerate.** Proposals: `python -m tests.support_swap --write` in `services/model`. Decisions: `NOEMA_WRITE_SWAP_DECISIONS=1 go test ./trust/decide -run TestSwapBothProposers` in `services/core`. Commit the result; never edit by hand.
