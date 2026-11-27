# EXP-05: the explainer's payment facts from the candidate item

## State
- Task: EXP-05 (A556 under A665, interim, relayed; A711 drops the recipient kind, relayed; A676). Owner: C2. Branch `feat/EXP-05-payment-facts-from-candidate`, PR noema#970.
- Done in this PR: `explanation/factsource/candidate.go`: the consumer-side `CandidateReader` port over the `candidates-response.v2` item, `PaymentSource.PaymentFacts` (three payment kinds, `payment.amount`, `account` and `payment.time`, through the one masking core) and `CaseFacts` (the three case kinds as unknowns, A556). The masking core (`factsource.go`) no longer holds a recipient row, field or function. `facts-inventory.md` lists no recipient kind.
- Fail closed: no item is three unknowns; an item that is not KNOWN (AMBIGUOUS included) is three unknowns; another tenant's item, another resource's item (an empty tenant or resource included), a reader error and a bad request are typed errors and no fact.
- Not in this PR (listed, not guessed):
  - **EL, composition:** an adapter that implements `CandidateReader` over the candidates read (noema#960, merged) and the sealed snapshot; the wiring of `PaymentSource` into the Explainer's `FactsSource`; `Config` (currency exponents from `contracts/currencies.json`, the tenant zone, owner queues) from the pack.
  - **EL, ledger reader (A665, A692):** the transaction-level version token and the `complete` marker in the ledger payload, in `evidence/portread`. Nothing here depends on it.
  - **Case kinds:** `case.ref`, `case.team`, `case.clock` stay unknowns until a Record reader for the receipt, the plan and `SLA_EVALUATED` exists.
  - **Recipient:** no kind, by A711 and A676. A sentence that needs the recipient has no fact; no pack template holds one, so no template changed and no pin moved.

## Session 2026-11-26 · EXP-05
## Session summary
- What changed (PR links): noema#970.
- Why: A711 (drop the recipient kind) and A676 (no name leaves the candidates read; #960 is merged) change the source from `candidates-response.v1` to `v2` and the payment kinds from four to three.
- Risks, and how they are contained: the dropped kind could come back through the core or a template; `TestNoRecipientNameOrKindIsInTheSourceOrTheCore`, `TestNoTemplateOrInventoryRowNamesTheDroppedKind` and `TestOnlyTheOutcomesDeclaredPaymentKindsAreEmitted` fail if it does.
- Tests added (REQ, INV, US or EC IDs): `candidate_test.go`, `dropped_kind_test.go` (REQ-EXP-001, REQ-EXP-002, REQ-SEC-004; INV-12, INV-13, INV-14, INV-19; AGENT-13).
- Regression cases added: `services/core/explanation/factsource/dropped_kind_test.go`.
- Debt entries: none.
- Conflicts raised: the earlier green (`29afc364a`, local only) read the v1 name member; A711 answers it.

## Pre-work for the next agent
- Exact next step: EL wires the adapter and `PaymentSource` in the composition; C2 adds the `fin.map.v1@3` version-token row once EL's `complete` marker is on `main`.
- Files to read first: `explanation/factsource/candidate.go`, `explanation/facts-inventory.md`.
- Do not touch: `evidence/portread` (EL's), the Record readers.
