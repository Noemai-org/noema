# Status · TRU-07: MAKER_CHECKER needs the approver role (defence in depth, `trust/confirm`)

Updated 2026-11-19. Owner: C1 (helper session). Branch `feat/TRU-07-maker-checker-role`, PR noema#793 (draft). Tier 1 (Trust confirmation path).

# Session 2026-11-19 · TRU-07 maker-checker role

## Session summary
- What changed (PR links): noema#793. `trust/confirm`: `eligible` now requires, for MAKER_CHECKER, an approver who is not the requester AND holds the role `ApproverRole`. New exported constant `confirm.ApproverRole = "approver"` (the value of `edge/session.RoleApprover`; a test compares the two). Exported names and signatures of the package are otherwise unchanged (`Resolve`, `Request`, `Event.ApproverRoles`, the `Resolution` values).
- Why: Security Reviewer's note on noema#750 (relayed by EL): `Resolve` made MAKER_CHECKER eligible for any other principal and did not look at a role; only the route (noema#754) kept a customer out. The role is now part of the eligibility rule too. REQ-TRU-009, INV-25, EC-U04, SoT 43.2a ("a second authorised person who is not the requester").
- Rule (fail closed): SELF = the requester; MAKER_CHECKER = not the requester and `ApproverRole` in `ApproverRoles`; ROLE:x = a holder of x. New fail-closed edges, each tested: no approver principal is never eligible (any requirement); MAKER_CHECKER with no recorded requester is never satisfied; `ROLE:` with an empty name never matches, and an empty string inside the role set never matches; roles are compared exactly (no case folding, no trimming); an unknown requirement form stays NotEligible.
- Risks, and how they are contained: (a) behaviour change for callers: a MAKER_CHECKER answer whose `Event.ApproverRoles` lacks "approver" is now APPROVER_NOT_ELIGIBLE. On main there is no caller of `confirm.Resolve` outside its tests; the adapter of noema#750/#752/#754 must put the authenticated session's role into `ApproverRoles` (from `session.Principal.Role`, never from the body). If it passes nil, every MAKER_CHECKER answer becomes NotEligible (safe, but visible). (b) Two edge cases are stricter than before and go beyond the note: SELF with both principals empty used to be eligible (`"" == ""`), and MAKER_CHECKER with an empty requester used to accept any approver. Both now fail closed.
- Tests added (REQ-TRU-009, INV-25, EC-U04; `maker_checker_role_test.go`): `TestApproverRoleIsTheSessionRole`; `TestMakerCheckerNeedsTheApproverRole` (17 named cases: customer, ops, audit, nil, empty, one empty name, requester with and without the role, three mis-spelled roles, a pack role of the same family, no approver, no requester); `TestEligibilityCrossProduct` (14 requirement forms x 2 requesters x 3 approvers x 16 role sets = 1344 combinations against an independently written rule); `TestEmptyRoleNeverSatisfiesRole`; `TestNoPrincipalWithoutTheApproverRoleIsEverMakerChecker` (property, seed 7, 30000 runs); `TestDeclineByACustomerIsNotEligible`. In `confirm_test.go` the two cases and the property oracle that encoded "MAKER_CHECKER needs no role" now give the approver the role (a rule change, not a weakening: the NotEligible side is stricter).
- Regression cases added: none (no defect fixed in a shipped path; the gap was a review finding on an open PR).
- Mutants run by hand on the green code, each killed, source restored after each: M1 role check dropped (killed by 4 tests); M2 requester check dropped (killed by 4); M3 empty roles accepted (killed by 4); M4 case-folding of the role name (killed by 3); M5 an empty role name matches (killed by 2); M6 an empty approver ID allowed (killed by 3).
- Debt entries: none. Conflicts raised: none (see doubt 1).

## Doubts and flags
1. The package comment said "Must never: ... hold ... a role name". `ApproverRole` is a platform role, not a pack role, so I narrowed that line to "a pack role name (the one platform role, ApproverRole, is the exception)". `trust/confirm` still imports only the standard library (the test, in the external test package, imports `edge/session` to pin the value; `lint` is run below). If EL prefers the value to be passed in rather than named here, the alternative is a field `ApproverRole` on `Request`; that would change an exported struct and was not taken.
2. Whether ROLE:<pack role> should also require the session role "approver" is not ruled: not required here (a session carries exactly one role, so a pack role arrives through the adapter's mapping). Not changed.
3. Rulings A362 to A376 are interim; none of their texts was available beyond the task brief.

## Pre-work for the next agent
- Exact next step: the confirmation-route lane (noema#750 adapter, #754 route, #752) sets `Event.ApproverRoles` from the authenticated session's role; keep the route's own customer-refusal test, it is still the first line.
- Open questions (owner): doubts 1 and 2 (EL).
- Files to read first: `services/core/trust/confirm/confirm.go`, `maker_checker_role_test.go`.
- Do not touch: the exported names of `trust/confirm`.
