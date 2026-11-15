-- mock-case schema, v4 (REQ-ENV-008, PERD REQ-EXE-004; Architect ruling A185, noema#522). SYNTHETIC. Forward-only; 0001 to 0003 are not edited.
--
-- Conditional complaint create. The capability log_complaint_reference is a CONDITIONAL_WRITE whose precondition is
--   not exists(complaint_ref for the subject and epoch).
-- A case is the subject and epoch (cases_subject_epoch_uq makes a case unique per transaction and epoch), so the precondition is "this case has no complaint". Execution can only
-- look before it writes, so two executions with different idempotency keys could both pass and log two irreversible complaints. A create that sends `If-None-Match: *` is
-- therefore made only if the case has none, in the transaction that inserts it, under a per-case lock that every complaint create takes.
-- This index is the database's own backstop for that rule: at most one conditional complaint per case, whatever the interleaving. A create without the header is unchanged
-- (conditional is false) and is not limited by it; a conditional create also refuses when an unconditional complaint already exists (checked under the lock).
ALTER TABLE complaints ADD COLUMN conditional boolean NOT NULL DEFAULT false;
CREATE UNIQUE INDEX complaints_conditional_case_uq ON complaints (tenant_id, case_id) WHERE conditional;
