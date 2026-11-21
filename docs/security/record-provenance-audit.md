# What the stored Record carries for audit and provenance

**7 Nov 2026 (Core Engineer 1).** As-built on `main` plus noema#243; not authority (the SoT, the PERD and the rulings decide). SYNTHETIC data only. The question: does the Record carry who, intent, evidence, policy version, decision, action, verification, outcome and times, including after a restart (S9) and a policy change during execution (S11)? The answer comes from running the real stack against the real mock-case and reading back through the audit path only (`record_read_chain_t`, `reconstruct.Build`): `services/core/execution/e2e/audit_record_test.go`.

## How to read a reconstruction (ruling A40)

A reconstruction is **consistent, not authenticated**. Every event hash chains to the one before and the chain verifies from genesis (tamper-evident), and the body is built only from those verified events. But nothing yet proves *which writer* appended an event: the database does not check a signature on a Record event, so a holder of one runtime credential could append a well-formed event under that credential's emitter (DEBT-03, REC-08, noema#236 shows it). Until DB-side signature verification (slice B, noema#229) covers receipts and REC-08 covers every Record event, a reconstruction is evidence of consistency, never of authorship. The response field `chain.verified: true` means the hash chain verified, not that the events are authenticated; whether the contract should say so is a question for the Engineering Lead (it would be a contract change, AGENT-15).

## Method

Events that exist on `main`: Execution (`REVALIDATION_PASSED`/`FAILED`, `GRANT_SPENT`, `DISPATCH_ATTEMPTED`, `RECEIPT`, `GRANT_VOIDED`, `SECURITY_EVENT`) and Outcome (`VERIFICATION_READ`, `VERDICT`, `WATCH_CREATED`, `RECOVERY_REQUESTED`, `SECURITY_EVENT`). Trust, Evidence, Edge and the Planner write **no** Record events on `main` (the grants in the e2e stack are planted by a test stand-in).

| Item | Where it is in the Record | Written by | State |
|---|---|---|---|
| **Who** (the principal) | `actor` of `SESSION_VALIDATED` / `INPUT_RECEIVED`; the grant names `principal_id`, but only in `decision_grant`, not in an Execution event | Edge (EDG-01) | **Not built.** Execution and Outcome events carry the service identity as `actor`, which is right for them (v3), so WHO is empty in a reconstruction on `main`. It must come from the authenticated session (INV-12) |
| **Intent** | `INTENT_ADMITTED` (`intent_node`, `intent_id`); `intent_hash` is in the grant only | Edge / Understanding / Trust | **Not built** on `main` |
| **Evidence** | `EVIDENCE_READ`, `EVIDENCE_REFRESHED`, `CONTEXT_SNAPSHOT_SEALED`; `evidence_set_hash` in the grant only | Evidence (noema#172 and the stack above it) | **Open PRs** |
| **Policy version** | `POLICY_EVALUATED` (`policy_id`, `policy_version`) | Trust (TRU-05) | **Not built.** Execution now adds the grant's `policy_version` to `GRANT_VOIDED` and the revalidation events (G3) |
| **Decision** | `DECISION_MADE` (`decision_outcome`, `decision_id`) | Trust (noema#197) | **Open PR** |
| **Action** | `GRANT_SPENT`, `DISPATCH_ATTEMPTED`, `RECEIPT` | Execution | **Built.** Gap G1 fixed here: the audit path refused every real receipt |
| **Verification** | `VERIFICATION_READ` | Outcome | **Built** (OUT-01) |
| **Outcome** | `VERDICT`, `WATCH_*`, `RECOVERY_REQUESTED` | Outcome | **Built** |
| **Times** | `at` of every event; `dispatched_at` in the receipt; `spent_at` in `grant_spend` | Appender, Execution | **Built.** Gap G2 fixed here: a swept receipt carried the sweep time as its dispatch time |

## Gaps found, and what noema#243 does

- **G1, ACTION unservable for every real write.** The controller wrote the idempotency key's logical action (`ESCALATE_CASE`) into the receipt's `logical_action`. `execution-receipt.v1` types that member as a pack ID and `reconstruct` derives `capability_id` from it, so `reconstruct.Build` refused every real receipt (500 `INTERNAL_SAFETY_BLOCK`). The reconstruct tests passed because their events were hand-made with pack IDs. Fix: the receipt's `logical_action` is the grant's capability; the key keeps its own action; the swept receipt takes the capability from the stored grant.
- **G2, S9 restart, times.** The sweeper's `OUTCOME_UNKNOWN` receipt carried `dispatched_at` = the sweep time. Fix: it is the committed spend time (`grant_spend.spent_at`); the time the receipt was written is the `RECEIPT` event's own `at`.
- **G3, S11 policy change.** `GRANT_VOIDED` held `grant_id` and `reason` only; the revalidation events held nothing or a changed-facts string. Fix: each carries the grant's `policy_version`, and the AUTHORITY projection serves it. The **new** active version is not recorded: the Trust port `DecisionStillAllowed` returns a boolean. That needs Trust to write `POLICY_PUBLISHED` (TRU-08) and `POLICY_EVALUATED`; until then a reconstruction shows the old version and the reason, not what replaced it.

## Not covered (other owners)

- WHO, intent, evidence, decision and policy-evaluation events come from modules that are open PRs or not built. `record/completeness` demands event *types* for governed writes, not their *content*, so a correlation can be "complete" while WHO and POLICY are empty. Whether completeness should demand content is a REC-04 decision for the Engineering Lead.
- S9 at the other two kill points: *after verify, before note* leaves `VERDICT` without `EXPLANATION_*` until Explanation exists; *before write* leaves only `REVALIDATION_PASSED` and no spend, which is correct (nothing happened) but looks like a request still in flight.
- Per-writer authentication of events (REC-08, DEBT-03): the Record is tamper-evident, not yet authenticated per writer.
