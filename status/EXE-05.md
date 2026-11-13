# Status · EXE-05 (Core Engineer 1) · evidence and complaint capabilities, end to end

Updated 2026-11-12. Plan task EXE-05: owner C1+AG; REQ-EXE-003, REQ-EXE-007, REQ-VER-001; Needs EXE-04, OUT-02; done when "each capability reaches ACHIEVED in a clean case and NOT_ACHIEVED in a planted failure". Allowlist extended to `services/core/adapters/**` by ruling A157 (DEC-2026-11-12-architect-rulings-a148-a160, relayed until docs#157 merges).

## State: slice 1 in review (#470); slice 2 (bridge dispatch and revalidation) opened stacked on it. EXE-05 is not Done.

## Session 2026-11-12 11:00 WAT · slice 1: the case adapter's evidence and complaint writes
### Session summary
- What changed (PR links): the EXE-05 slice 1 pull request on `feat/EXE-05-evidence-complaint-adapter`: `adapters.CaseRecords` (`adapters/case.go`) and its implementation in `CaseClient` (`adapters/synthetic_tushe/records.go`): `AppendEvidence`, `LogComplaint`, `GetEvidence`, `GetComplaint`, `FindEvidence`, `FindComplaints`.
- Why: mock-case serves the evidence and complaint rows of PERD 34.4, but the core's client had no method for them, so `execution/bridge` could dispatch only `create_escalation_case` and `route_to_internal_owner`. noema#456.
- Risks, and how they are contained: an acknowledgement taken for a write it was not (a 201 for another case, tenant, key or payload) is `NO_RESPONSE`/UNRECOGNISED, never `ACK_2XX`; a read that is not the record asked for is `MALFORMED`; the payload hash is sent so a changed payload is refused by the target. The interface is separate from `adapters.Case`, so no existing implementation or fake changes.
- Tests added (REQ-EXE-006, REQ-EXE-007, INV-04, INV-07, EC-E06, EVD-05): `synthetic_tushe/records_test.go` (11) and `outcome_e2e/records_test.go` (1, against the real mock-case). 10 mutants of `records.go` were run; each is killed (five survived at first, which added `TestARecordReadThatIsNotTheRecordAskedForIsMalformed` and `TestAPageWithAnInconsistentCompletenessMarkerIsMalformed`).
- Regression cases added: none. Debt entries: none. Conflicts raised: none.
- Not in this slice: the dispatch mapping in `execution/bridge` for the two capabilities; the capability descriptors in `packs/fintech/capabilities/**` (one Founder approval, A157); the Outcome predicates for evidence and complaints (OUT-02, C2, A157). No fault-plan test of the new writes: their transport classification is the shared `classifyCommon` that the create and route tests already cover with recorded faults.

### Pre-work for the next agent
- Exact next step (slice 2, `execution/bridge`): map the two capabilities in `CaseDispatcher` with a closed parameter set each, and an end-to-end test through Outcome once OUT-02's predicates exist. The descriptors first: nothing registers a capability whose descriptor is not in the pack.
- Open questions (owner): the descriptor files and their parameter names (the pack owner, one Founder); the Outcome predicates (C2).
- Files to read first: `services/core/adapters/synthetic_tushe/records.go`, `services/core/execution/bridge/dispatch.go`.
- Do not touch: the migrations.

## Session 2026-11-13 · slice 1: EL's independent read and the Security Reviewer's notes (noema#470)
### Session summary
- What changed (PR links): noema#470: (1) the write calls (`CreateCase`, `RouteCase`, `AppendEvidence`, `LogComplaint`) go through a second client whose transport reuses no connection (`noReuse`): Go's `net/http` sends a POST that carries an `Idempotency-Key` again, on its own, when a reused keep-alive connection fails before any byte of the answer, so "one request per write" was false below the adapter (EL, P1, REQ-EXE-006, AGENT-08). A caller-supplied RoundTripper that is not an `*http.Transport` is used as it is. (2) `AppendEvidence` refuses, before sending, a payload over `maxEvidencePayload` (the response bound over eight: the target echoes the payload, an escaped byte is at most six) and a payload that is not valid UTF-8 (EL, P2). (3) `FindEvidence` and `FindComplaints` refuse an item outside the query (another case, another key) and a page over its `Limit` (EL P2; the Security Reviewer's note 3). (4) the weak assertions of EL's finding 4 are strict: `MALFORMED` and `INVALID_INPUT` are asserted by class, and the exact-bytes test sends three spellings and checks each request's hash. (5) The Security Reviewer's note 2: `EvidenceRecord.Payload` says the caller keeps account data out of it.
- Why: the reviewers' findings on `656dd1c0`; one push for the round (A175, A176).
- Tests, failing first (run locally before the fix, red for the right reasons): `TestAWriteIsNeverSentTwiceByTheTransportOnAReusedConnection` (the second write reached the server twice: evidence, complaint and create), `TestAnEvidencePayloadThatCannotBeEchoedOrIsNotUTF8IsRefusedBeforeSending` (three requests were sent for refused payloads), `TestAFindRefusesAnItemOutsideItsQueryAndAPageOverItsLimit` (six cases accepted). Mutants, each caught: writes through the reading client; keep-alives left on; no size cap; the cap too high; no UTF-8 check; evidence and complaints not scoped; the limit ignored.
- Not claimed: HTTPS with HTTP/2 (the mock-case listener is plain HTTP in the POC; HTTP/2 multiplexes on one connection and its replay rules differ); the Security Reviewer's note 1 (the masking of a complaint reference where it is logged or recorded) is carried by the consumer, noema#491.
- Debt entries: none. Conflicts raised: none.

## Session 2026-11-12 14:00 WAT · slice 2: the bridge dispatches and revalidates evidence and complaint
### Session summary
- What changed (PR links): the EXE-05 slice 2 pull request on `feat/EXE-05-bridge-dispatch`, stacked on #470: `execution/bridge/dispatch.go` maps `fin.cap.record_investigation_evidence` and `fin.cap.log_complaint_reference` to `adapters.CaseRecords` (closed parameter sets, `case_id` must equal the grant's target); `revalidate.go` reads `case_status` for both and `existing_complaint_reference` for the complaint.
- Why: A157 (Write path for US-06). The descriptors, input schemas and outcome definitions for both capabilities are already in `packs/fintech_v1.json` and `packs/schemas/fin/`; this slice adds nothing to the pack, so no Founder approval of descriptors is needed for it.
- Risks, and how they are contained: a plan that names another case than the grant's target is a terminal adapter defect and makes no call (INV-03); an incomplete complaint page is an error, never a pass (the controller voids the grant); with no records adapter the capabilities have no mapping. Honest gap: the complaint write is not conditional on absence at mock-case, so a rival complaint written between E7 and E8 is not stopped by the target. E7 and the Outcome predicate (`count == 1`) are the guards; no test claims more.
- Tests added (REQ-EXE-003, REQ-EXE-004, REQ-EXE-006, REQ-CAP-002, INV-03, EC-E06): `execution/bridge/records_test.go` (6) and `execution/e2e/records_test.go` (3, through the controller over Postgres against the real mock-case). 9 mutants of the bridge were run; each is killed. Record events asserted: GRANT_SPENT, DISPATCH_ATTEMPTED, RECEIPT, REVALIDATION_PASSED for the write; REVALIDATION_FAILED and a RecoveryRequest for the voided second complaint.
- Regression cases added: none. Debt entries: none. Conflicts raised: none.
- Not in this slice, and so not claimable: ACHIEVED / NOT_ACHIEVED for these capabilities (the plan's EXE-05 done-when). That needs OUT-02's predicates and readers for `evidence_recorded` and `complaint_logged` (C2, A157). A receipt says only that mock-case acknowledged.

### Pre-work for the next agent
- Exact next step: once OUT-02 has the predicates, add an end-to-end case through Outcome: clean case gives ACHIEVED; a planted failure (a dropped append, a second complaint) gives NOT_ACHIEVED.
- Open questions (owner): whether mock-case should make the complaint create conditional on absence (Tushe delta, Architect).
- Files to read first: `services/core/execution/bridge/dispatch.go`, `revalidate.go`, `execution/e2e/records_test.go`.
- Do not touch: the migrations.
