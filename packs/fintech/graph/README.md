# packs/fintech/graph

**Implements:** REQ-CAP-001, REQ-AG-001, REQ-AG-002, REQ-NFR-005.
**Owning task:** PCK-01 (the rewrite of the fintech pack), T2 of DEC-2026-10-20, and PCK-04 (graph shape).

The fintech pack's Enterprise Action Graph (PERD §24): pack data, validated by `packcheck`, listed in `packs/fintech_v1.json` under `action_graph_files`. Packs are data, loaded and never imported as code (SoT §51). Each file is an `action-graph.v2.json` document.

| File | Graph | Roots | Shape |
|---|---|---|---|
| `g1_investigate.json` | `fin.graph.investigate` (always) | `lookup_transaction` | 7 nodes (5 READ, 2 INTERNAL), 9 edges: the transaction lookup fans out to four reads, which join at `reconcile_transaction_state`, then `evaluate_sla_status` |
| `g2_escalate.json` | `fin.graph.escalate` | `create_escalation_case` | 3 WRITE nodes in a chain: create the case, attach the evidence, route it |
| `g3_complaint.json` | `fin.graph.complaint` | `log_complaint_reference` | 1 WRITE node |

**Postconditions of the two lookups (PCK-04, ruling D6 of 5 Nov 2026).** `lookup_beneficiary` has `exists(beneficiary where beneficiary.transaction == subject_resource)`, typed against a `beneficiary` descriptor on `fin.src.beneficiary` (authoritative for `beneficiary_list_and_aliases`). `lookup_account` has an ownership-only postcondition, `exists(account where account.owner == principal_id)`, typed against an `account` descriptor on `fin.src.customer` (authoritative for `customer_identity_and_ownership`). `principal_id` is a predicate variable the evaluator must bind from the authenticated session (INV-12); nothing binds it yet.

**Known gap.** There is no postcondition on an account's balance or state: no source in the pack covers them, and `fin.src.core_ledger` is authoritative only for `debit_posted_or_reversed`. It is an open gap, recorded for a request to Tushe and a Tier 1 debt row (the Repo Steward files them), not a closed one.

Created by T1 of DEC-2026-10-20.
