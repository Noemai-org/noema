-- mock-case schema, v3 (REQ-ENV-008, PERD REQ-EXE-004). SYNTHETIC. Forward-only; 0001 and 0002 are not edited.
--
-- Create-if-absent. The capability create_escalation_case is a CONDITIONAL_WRITE: the graph's precondition is
--   not exists(case where case.subject == subject_resource and case.epoch == logical_resolution_epoch
--              and case.status in ['OPEN','ROUTED'])
-- A client-side read cannot make that atomic: two creators can both read "absent" and both write. This index makes the
-- target refuse the second one, in the same transaction that would have inserted it. The subject of a case is its
-- transaction (transaction_ref) inside a tenant; the epoch separates a later case on the same transaction (PERD 27.2).
-- A case is only ever OPEN or ROUTED, so every row counts; the WHERE clause keeps the statement of the precondition.
CREATE UNIQUE INDEX cases_subject_epoch_uq ON cases (tenant_id, transaction_ref, epoch) WHERE status IN ('OPEN', 'ROUTED');
