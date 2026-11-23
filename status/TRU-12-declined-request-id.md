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

# Session 2026-11-22 03:50 WAT · TRU-12 (C1, ruling A507: the declined request is checked for tenant and intent ownership)

## Session summary
- What changed (PR links): `trust/recorder`: for a REJECT that names a declined request (`Request.DeclinedRequestID`), `resolveState` reads `confirmation_request` as the tenant (`SELECT intent_id ... WHERE tenant_id AND confirmation_request_id`; row security hides another tenant's) and refuses with the new typed `ErrDeclinedRequestNotValid` (fixed text, nothing written) when the request is absent for the tenant or its `intent_id` is not the decision's. The shape check and the rule that no other outcome names a request are unchanged; a decline that names no request is still valid (A470).
- Why: the Security Reviewer's condition on noema#849 and #840, accepted as a follow-up owned by C1 (A507). Before this, a well-formed ID that named no request, another intent's or another tenant's was recorded as the REJECT's state, and two declines of one input could rest on different IDs.
- Risks, and how they are contained: a fail-closed check on the decline path only. It needs the request row to exist, which the confirmation store (`trustpg2.Requests.Create`) writes and the approver's answer references; the Writer's own CONFIRM path writes the event and no row (found while writing the tests: a request created only by the Writer's CONFIRM cannot be declined by this path until the store has its row). `RecorderAdapter` still does not hand `DeclinedRequestID` to the Writer; that mapping is the next small PR and must land after this one.
- Tests added (REQ-TRU-009, REQ-SEC-004; INV-25, INV-12, EC-E16): `declined_request_ownership_test.go`: the real request of the intent is accepted and is the REJECT's state; a request that does not exist, one of another intent and one of another tenant are each refused with nothing written; no request named stays valid; a malformed name stays `ErrRequest`. `TestADeclinedRejectOnTheTupleOfAStoredConfirm...` now seeds its declined requests first (the same assertions). Two hand mutants killed and restored (the intent comparison dropped; a missing row accepted).
- Regression cases added: the same. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the adapter maps `DeclinedRequestID` into `recorder.Request` (a small PR after this one); whether the Writer should also require a recorded DECLINE answer (the answer table of 0032) is the Architect's; I started with existence and intent, as proposed.
- Files to read first: `trust/recorder/inputkey.go` (`resolveState`), `migrations/0032_confirmation_request_answer.sql`, `orchestrator/portadapters/trustpg2/requests.go`.
- Do not touch: `services/core/migrations/` (no change needed).

# Session 2026-11-23 WAT · TRU-12 (the Recorder adapter maps DeclinedRequestID, A470, A507)

## Session summary
- What changed (PR links): `orchestrator/portadapters/trust/recorder.go` maps `intake.RecordRequest.DeclinedRequestID` to `recorder.Request.DeclinedRequestID`. `declinedShape` (called first in `outcomeShape`) refuses with `ErrRecordRequest`, before any transaction, a request that names one on any decision but a REJECT with the reason DECLINED_BY_APPROVER, and one whose id is not `cfr_` plus 2 to 32 alphanumerics. PR link: to be added when EL opens it.
- Why: Founder instruction (interim, relayed, not yet committed; A470, A507, A519). The field (noema#849) and the Writer's tenant and intent check (A507) are on main; the adapter did not hand the id on, so a declined REJECT was stored without its request.
- Safer readings taken: a REJECT for another reason that names a request is refused (the instruction only lets a declined REJECT carry one; the Writer would accept any REJECT); the adapter checks the id's shape itself, as the Writer does, so a malformed id never opens a transaction.
- Risks, and how they are contained: fail-closed, adapter-side only; the Writer still reads the request as the tenant and compares its intent. No contract, trust/** or migration change.
- Tests added (REQ-TRU-009, REQ-STM-001; INV-03): `recorder_declined_test.go`: four tests on the adapter's own fakes (no mocked failure mode): id reaches the Writer unchanged; REJECT without one maps empty (declined and two other reasons); ALLOW, CONFIRM, UNKNOWN, ESCALATE, another REJECT reason and two non-REJECT with the declined reason are refused before any transaction; eight malformed ids refused. Seen failing locally before the change (red commit 2626b45f); CI reading is EL's.
- Mutants (hand, 5 plus 1 added after a survivor; all killed): mapping dropped; outcome check dropped (survived until the two non-REJECT-with-declined-reason cases were added); reason check dropped; shape check dropped; empty-check inverted.
- Regression cases added: the same. Debt entries: none. Conflicts raised: none.
- Local run: gofmt, vet, lint, hard_code guard clean; package -race run: the 33 failures are the pgsodium database tests (CI only), none other.

## Pre-work for the next agent
- Exact next step: EL pushes the green commit and opens the PR.
- Open questions (owner): whether the Writer should also require a recorded DECLINE answer (Architect, noted in the A507 session).
- Files to read first: `orchestrator/portadapters/trust/recorder.go` (`declinedShape`), `trust/recorder/inputkey.go`.
- Do not touch: `trust/recorder`, `trust/intake`.
