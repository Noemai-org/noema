# Status · EVD-06 · event time, ingestion time and enterprise strings

Owner: C2 · Task: EVD-06 (REQ-CTX-005, REQ-CTX-007). Date: 2026-11-17.

# Session 2026-11-17 · EVD-06 slice 1 · the permutation property of Reconcile (REQ-CTX-005)

## Session summary
- What changed: `services/core/trust/reconcile/permutation_test.go`, a test only. It gives `reconcile.Reconcile` every arrival order of 400 seeded random evidence sets of 2 to 6 items (all five entities, all eight statuses, fields set and unset, repeated debits inside and outside the 600 s duplicate window) over the real rule set of the fintech pack, and requires the whole answer (classification, rule, contributing items, contradictions, the time it holds until, and an error if any) to equal the first order's. A second test requires that Reconcile changes none of the evidence it is given.
- Why: REQ-CTX-005's acceptance criterion, "permuting the arrival order of the same evidence set SHALL yield the identical classification (property test)"; the Go property test the requirement names.
- Evidence: no red run is possible (the behaviour already holds); mutants instead. Removing the sort of the evidence by id at the top of `Reconcile` fails the property at the first affected set (a contradiction list in another order). Four sorts below it are not killed, and are stated: three of them (the contradictions' order, the duplicate keys, the time order inside a duplicate group) act on lists that are already in id order, so permuting the arrival order cannot reach them; they are about the correctness of the clusters, not about order. The test says nothing on them.
- Risks, and how they are contained: a test only, no production file. It uses `math/rand` with a fixed seed and no new dependency (`pgregory.net/rapid` is [REC] and needs approval, AGENT-14).
- Tests added (REQ-CTX-005, INV-21): 2. Regression cases added: none. Debt entries: none. Conflicts raised: none.

## What this does not show, and what is next in EVD-06
- Late or out-of-order evidence never rolls state back without causal evidence, and the pack's skew tolerance with EVENT_ORDER_UNCERTAIN (EC-S16 to EC-S18): those are about `effective_at`, not arrival order, and need the benchmark's cases and the rule set's ordering rules; not done here.
- REQ-CTX-007 (enterprise strings delimited, capped at 256 characters, control characters stripped, an advisory injection detector from a pattern list in pack data that records `SECURITY_EVENT: INJECTION_DETECTED`): not started. It needs two decisions first (below).

## Pre-work for the next agent
- Exact next step: slice 2 for REQ-CTX-007, after the Architect says where the detector lives and which pack file holds its pattern list.
- Open questions (owner): (1) the detector's place: core (an Evidence Layer step that records the event) or the model service (before a prompt is built); the Record is the core's, so the event must be written by the core (Architect). (2) The pattern list: a new pack file and a contract, or an existing member (Architect, Steward for the OD entry).
- Files to read first: `services/core/trust/reconcile/reconcile.go`, `status/EVD-06.md`.
- Do not touch: the rule set's rows (the Architect approves them).
