# Session 2026-11-23 · TRU-12 · the Writer refuses a decline that names no request (A597, A600)

## Session summary
- What changed (PR links): noema#930. `trust/recorder.Writer.check` refuses a REJECT whose reason codes include DECLINED_BY_APPROVER and whose `DeclinedRequestID` is empty with `ErrRequest`, before any statement is sent on the transaction. Every other REJECT may still name none.
- Why: A542 (a decline rests on the request it declined), A597 and A600 (the Writer is the last gate before the Record; the intake and the adapter refuse it already, #912).
- Risks, and how they are contained: a caller that wrote a decline with no request now gets ErrRequest. Two tests had that premise: the stored-decline test now names a seeded request, and the conflict test's REJECT rests on no confirmation so it uses a non-decline reason (NOT_AUTHORIZED).
- Tests added (REQ-TRU-009; INV-03, INV-12, EC-E16): `declined_required_test.go` (a transaction on which any call panics proves nothing is sent).
- Regression cases added: the test above. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: none for this task. A581, A590, A598 (the early-decision key with turn, tenant and verified principal, migration 0040) wait for #923 on main.
- Do not touch: the intake and adapter refusals of #912.
