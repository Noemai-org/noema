# packs/fintech/graph

**Implements:** REQ-CAP-001, REQ-AG-001, REQ-AG-002, REQ-NFR-005.
**Owning task:** PCK-01 (the rewrite of the fintech pack), T2 of DEC-2026-10-20, and PCK-04 (graph shape).

The fintech pack's Enterprise Action Graph (PERD §24): pack data, validated by `packcheck`, listed in `packs/fintech_v1.json` under `action_graph_files`. Packs are data, loaded and never imported as code (SoT §51). Each file is an `action-graph.v2.json` document.

| File | Graph | Roots | Shape |
|---|---|---|---|
| `g1_investigate.json` | `fin.graph.investigate` (always) | `lookup_transaction` | 7 nodes (5 READ, 2 INTERNAL), 9 edges: the transaction lookup fans out to four reads, which join at `reconcile_transaction_state`, then `evaluate_sla_status` |
| `g2_escalate.json` | `fin.graph.escalate` | `create_escalation_case` | 3 WRITE nodes in a chain: create the case, attach the evidence, route it |
| `g3_complaint.json` | `fin.graph.complaint` | `log_complaint_reference` | 1 WRITE node |

**Known gap (PCK-04, 1 Nov 2026).** `lookup_beneficiary` and `lookup_account` in `g1_investigate.json` have no postcondition, so `packcheck` fails the pack (REQ-AG-001 as amended). A postcondition must type-check against an evidence descriptor, and the pack has none for a beneficiary or an account. Core Engineer 2 adds them from the signed Tushe worksheet only (Architect, 1 Nov 2026, E2); until then the pack does not pass, and it is not exempted.

Created by T1 of DEC-2026-10-20.
