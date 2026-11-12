# Status · EXE-05 (Core Engineer 1) · evidence and complaint capabilities, end to end

Updated 2026-11-12. Plan task EXE-05: owner C1+AG; REQ-EXE-003, REQ-EXE-007, REQ-VER-001; Needs EXE-04, OUT-02; done when "each capability reaches ACHIEVED in a clean case and NOT_ACHIEVED in a planted failure". Allowlist extended to `services/core/adapters/**` by ruling A157 (DEC-2026-11-12-architect-rulings-a148-a160, relayed until docs#157 merges).

## State: slice 1 in review. EXE-05 is not Done.

## Session 2026-11-12 11:00 WAT · slice 1: the case adapter's evidence and complaint writes
### Session summary
- What changed (PR links): the EXE-05 slice 1 pull request on `feat/EXE-05-evidence-complaint-adapter`: `adapters.CaseRecords` (`adapters/case.go`) and its implementation in `CaseClient` (`adapters/synthetic_tushe/records.go`): `AppendEvidence`, `LogComplaint`, `GetEvidence`, `GetComplaint`, `FindEvidence`, `FindComplaints`.
- Why: mock-case serves the evidence and complaint rows of PERD 34.4, but the core's client had no method for them, so `execution/bridge` could dispatch only `create_escalation_case` and `route_to_internal_owner`. noema#456.
- Risks, and how they are contained: an acknowledgement taken for a write it was not (a 201 for another case, tenant, key or payload) is `NO_RESPONSE`/UNRECOGNISED, never `ACK_2XX`; a read that is not the record asked for is `MALFORMED`; the payload hash is sent so a changed payload is refused by the target. The interface is separate from `adapters.Case`, so no existing implementation or fake changes.
- Tests added (REQ-EXE-006, REQ-EXE-007, INV-04, INV-07, EC-E06, EVD-05): `synthetic_tushe/records_test.go` (11) and `outcome_e2e/records_test.go` (1, against the real mock-case). 13 mutants of `records.go` were run; each is killed (five survived at first, which added `TestARecordReadThatIsNotTheRecordAskedForIsMalformed` and `TestAPageWithAnInconsistentCompletenessMarkerIsMalformed`).
- Regression cases added: none. Debt entries: none. Conflicts raised: none.
- Not in this slice: the dispatch mapping in `execution/bridge` for the two capabilities; the capability descriptors in `packs/fintech/capabilities/**` (one Founder approval, A157); the Outcome predicates for evidence and complaints (OUT-02, C2, A157). No fault-plan test of the new writes: their transport classification is the shared `classifyCommon` that the create and route tests already cover with recorded faults.

### Pre-work for the next agent
- Exact next step (slice 2, `execution/bridge`): map the two capabilities in `CaseDispatcher` with a closed parameter set each, and an end-to-end test through Outcome once OUT-02's predicates exist. The descriptors first: nothing registers a capability whose descriptor is not in the pack.
- Open questions (owner): the descriptor files and their parameter names (the pack owner, one Founder); the Outcome predicates (C2).
- Files to read first: `services/core/adapters/synthetic_tushe/records.go`, `services/core/execution/bridge/dispatch.go`.
- Do not touch: the migrations.
