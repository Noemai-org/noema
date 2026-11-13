-- TRU-05 follow-up (rulings A172 and A186; PERD REQ-TRU-007, REQ-TRU-008; INV-21, INV-25): a Decision Record exists once for its full input tuple: the
-- tenant, the intent ID, the intent hash, the sealed context snapshot and the policy version. The same tuple can never hold two decisions, so a retried or
-- resumed run reads the stored one (trust/recorder: Writer.Write) instead of deciding again, and a second grant cannot be issued for one input. A new
-- snapshot, another policy version, another intent hash or another intent is another input and another decision.
--
-- The tenant and the intent hash are columns; the intent ID, the snapshot and the policy version are members of the signed document (decision-record.v2),
-- so the index is over expressions. tenant_id is first, so it is also the tenant-scoped lookup path of the writer. A document with no snapshot member is NULL
-- in the index and is not keyed: decision-record.v2 requires the snapshot and the writer refuses a request without one, so only the direct inserts of the
-- schema tests lack it. If rows already hold a duplicate tuple this migration fails and the database must be looked at by a person; nothing is deleted
-- (decision_record is insert-only).
CREATE UNIQUE INDEX decision_record_input_key ON decision_record
  (tenant_id, (json ->> 'intent_id'), intent_hash, (json ->> 'evidence_snapshot_id'), (json ->> 'policy_version'));
