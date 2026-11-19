# Status · TRU-07 A306 · per-step policy evaluation at T11 (Core Engineer 1, prepared locally)

Updated 2026-11-18. Follow-up to noema#572 (merged as is): the Security Reviewer's note 1 and Codex's architecture note on #572. Task A306 (relayed by the user, interim: not a committed decision). Tier 1 (trust is a P0 path).

## State: two local commits on `prep/a306-per-step-policy`; not pushed, no PR

# Session 2026-11-18 · TRU-07 A306

## Session summary
- What changed (PR links): none yet. `trust/intake`: T11 asks the `Policies` port once for each capability of the authorized set (the intent's own plus the pack action graph's, sorted) when the request leads to a write, and combines the answers into the one T11 outcome (`policy_combine.go`); the authorized set is read once, by T11 first (the memoized `authorizedCapabilities`, unchanged); comments and README say exactly what is evaluated.
- Why: T11 evaluated the policy for the intent's own capability only (`PolicyQuery.CapabilityID = e.capability.ID`), while T12 budget-checks and T14 `planAgrees` authorizes the graph's other capabilities by role and registry only. A tenant policy that denies a capability for a role was therefore bypassed by a graph step.
- Risks, and how they are contained: it only adds refusals (more REJECT, ESCALATE, CONFIRM or WAIT, or UNKNOWN); it adds no allow path and widens nothing. The one real consequence is for policy data, see Questions. A policy error, a panic, an unreadable set or an unusable answer for any single capability is UNKNOWN `INTERNAL_ERROR`; nothing is recorded in the basis on a failed evaluation.
- Tests added (REQ-TRU-004, REQ-TRU-003, REQ-TRU-007, REQ-NFR-002; INV-03, INV-09, INV-16, INV-21): `policy_capabilities_test.go`, 17 tests. Red first at `e64e113e` (12 failed for the right reason: T11 asked about one capability; the other five are guards that hold on the old code: read, no capability, shared read of the set, the two Dispatches contract tests). Mutants (12), all killed: skip the extra capabilities; ESCALATE over REJECT; WAIT over CONFIRM; PERMIT first; PermitMatched from the root only; no de-duplication of rules; none of versions; no per-capability default deny; the last capability gives the reason; WAIT at the latest time; a read evaluates the whole set; only the root's rules recorded.
- Regression cases added: the same.
- Debt entries: none. `policy_capabilities_test.go` is over 400 lines (a table-driven suite and its fake; one file for one behaviour, review trigger noted).
- Conflicts raised: none. PERD 23.3 and 23.4 are silent on the combination (below); the default below is the safer reading, for the Architect.

## The combination rule (interim, for the Architect)
PERD 23.4 defines one capability's result: the class ladder, then REJECT > ESCALATE > CONFIRM > WAIT > PERMIT within the winning class, and no matching PERMIT is REJECT `NO_PERMITTING_RULE`. PERD 23.3 gives T11 the stop set {REJECT, ESCALATE, CONFIRM, WAIT} and T14 only UNKNOWN. Neither says what several capabilities do together, so, per the task's proposed default:
1. Each capability is evaluated by the `Policies` port as before and keeps its own default deny (a PERMIT that names no rule, a REJECT with no reason code, a CONFIRM with no PERMIT matched are REJECT `NO_PERMITTING_RULE`), applied before combining so that one capability's rules never make up for another's.
2. The most restrictive effect wins: REJECT over ESCALATE over CONFIRM over WAIT over PERMIT (the within-class order of PERD 23.4, used across capabilities; the order is a local copy because `policy.effectOrder` is unexported, and a test writes it out). A capability with no matching rule is REJECT as today.
3. Tie-break (not in the task, my choice; deterministic, INV-21): of equal effects the first capability in sorted order supplies the reason code; two WAITs: the earliest `reevaluate_at`, with that capability's reason. A re-evaluation is a fresh full evaluation, so the earliest time is never unsafe.
4. `PermitMatched` is true only if every capability had a PERMIT matched, so a CONFIRM stands only then (INV-03).
5. The rules and the policy versions of every capability are recorded in the basis (INV-09), each once, in order of first appearance.
6. Fail closed: an effect that is none of the five for any capability is UNKNOWN even when another capability would REJECT.

The `Policies` port is unchanged (the smaller change): `Evaluate` is called once per capability with `PolicyQuery.CapabilityID` set, so the adapters need no new field and the combination stays in the intake, testable without a database. The alternative (a query with a list of capabilities) would move the combination into every adapter.

## Questions for the Architect (PERD silent; defaults applied)
- Q1 The combination rule above (2 to 4).
- Q2 Policy data: with this change every capability of a pack's action graph needs a PERMIT rule in the tenant policy, or a write that reaches it is REJECT `NO_PERMITTING_RULE` (default deny, PERD 23.4). The pack and policy owners must cover the graph before the G1 run, or every multi-step write is REJECT.
- Q3 What is a "read" at T11: I used `late.needsWrite` (T3), because a plan has steps only then (`wellFormed`), so no graph step can exist for a read. `dispatchBudget` (T12) treats a request as a write when the intent names a capability or needsWrite is true, which differs for a request with a capability and no write. Not changed here; the two predicates should be one decision.
- Q4 A write that names no capability and has an empty authorized set is evaluated once with an empty `CapabilityID`, as before (the intent node alone).
- Q5 The policy is asked about every capability even after one REJECT, so the basis records all of them and an error for any capability fails closed; N queries for N capabilities.

## Wiring for EL's production adapters (also in `status/TRU-07-intake-t13-t15.md`)
See the section added there on 2026-11-18.

## Pre-work for the next agent
- Exact next step: the Architect answers Q1 to Q5; then push, open the PR (task A306, REQ-TRU-004, REQ-TRU-003, REQ-TRU-007; Tier 1, so Codex, SR and one independent role review); the PR body needs the plan, the SoT 52 questions and the checklist.
- Open questions (owner): Q1 to Q5 (Architect); the pack and policy owners cover the graph with PERMIT rules (Q2).
- Files to read first: `trust/intake/policy_combine.go`, `steps_t10_t12.go` (`t11`, `policyCapabilities`), `policy_capabilities_test.go`.
- Do not touch: the stop sets of PERD 23.3; `trust/policy` and `trust/decide`.
