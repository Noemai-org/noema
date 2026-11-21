# Session 2026-11-20 17:00 WAT · TRU-12 (A470, DeclinedRequestID)

## Session summary
- What changed (PR links): `intake.RecordRequest` gains the optional string member `DeclinedRequestID` (noema#849). T13's declined path (noema#755) now passes the answer's `RequestID` in it, so the Recorder's writer can store a REJECT DECLINED_BY_APPROVER with the declined request it rests on; every other decision leaves it empty. The Recorder port's doc comment states what each outcome carries (A456, A470).
- Why: A470 (interim, docs#216): "C1 adds an optional DeclinedRequestID to intake.RecordRequest, plus EL's doc line." noema#840 (EL) maps the field to the Recorder's `Request.DeclinedRequestID` and says a REJECT is recorded without one until this lands.
- Reading taken: A470 names the field and not its source; the only request id a decline has is the answer's own, so T13 passes it (empty stays empty: the answer's request id is optional on a decline, as T13 already allows).
- Risks, and how they are contained: the writer shape-checks the id and refuses it on any outcome but a REJECT (trust/recorder, `Request.DeclinedRequestID`); T13 already refuses a malformed id before this point. The change touches only trust/intake.
- Tests added (REQ-TRU-002, REQ-TRU-009; INV-25, INV-03, EC-E16): `declined_request_id_test.go`: the member exists and is an optional string; an ALLOW and a CONFIRM with an open request leave it empty; a declined REJECT carries the declined request, and a decline that names none leaves it empty. Seen failing in CI: RED `7a6fe5b9` (no member: `trust/intake` shard failed on the two member tests). RED `c8d55314` (declined REJECT without the id): CI shard `tests (2)` failed on `TestADeclinedRejectNamesTheDeclinedRequest` alone (`DeclinedRequestID ""`, want `cfr_synth0001`); the member tests passed. A first version of the CONFIRM case was set up wrongly (UNKNOWN, not CONFIRM) and never ran because the member test failed first; it was corrected in the next commit with the setup the other T13 tests use.
- Regression cases added: none (a new member, not a defect).
- Debt entries: none.
- Conflicts raised: none.
- Mutants (hand, restored with git checkout): `DeclinedRequestID` set on an ALLOW (line of T15) and on a CONFIRM (T13) are killed by the new test; set on the declined path is the intended behaviour (the first run of this mutant survived a RED-only state, which is why the declined test was added).
- EL's doc line: not found in any thread I can read; the Recorder port's doc comment follows A456 and A470 as recorded; EL may reword it.

## Pre-work for the next agent
- Exact next step: noema#840 (EL) maps `RecordRequest.DeclinedRequestID` to `recorder.Request.DeclinedRequestID` in `orchestrator/portadapters/trust/recorder.go`.
- Open questions (owner): the stale comment above `t13` in `gates_t13_t15.go` still says a decline ends in UNKNOWN (EL/C1, a doc fix in a later PR); EL's doc line text (EL).
- Files to read first: `trust/intake/ports_t13_t15.go`, `trust/intake/gates_t13_t15.go` (t13Declined), `trust/recorder/recorder.go` (DeclinedRequestID).
- Do not touch: `trust/recorder`, `orchestrator/portadapters/trust` (EL's noema#840).
