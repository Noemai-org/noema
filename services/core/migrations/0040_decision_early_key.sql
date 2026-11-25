-- 0040: an early decision names the turn it answers and the verified principal (TRU-10; rulings A663, A581 and A590, interim, relayed and recorded in
-- DEC-2026-11-23-architect-rulings-a581-a592 and DEC-2026-11-25-architect-rulings-a657-a667; PERD REQ-TRU-001, REQ-TRU-007, REQ-TRU-010; INV-03, INV-17, INV-21, INV-25).
-- Number 0040 is the one the ledger reserved for A581. Forward-only: never edited after merge. No CREATE ROLE here (TestNoMigrationAfter0001CreatesARole).
--
-- An ESCALATE reached before the evidence and the policy were evaluated is recorded with its three basis members null (0038; A574, A577). With a null snapshot and a null policy, the
-- single-decision key of 0038 is the intent and its hash alone, so a later turn on the same intent would be handed a stale decision. A581 keys such a decision by the turn (the client's
-- request ID) too, and A590 adds the verified principal. Two changes, both on decision_record, no row is written and no trigger fires:
--
-- 1. turn_id and principal_id: both NULL for every decision that is not an early one, and for an early one a lower-case UUID and a principal ID of common.v1. decision_record_early_key_shape
--    refuses one without the other, a malformed value, a turn on an ALLOW or a CONFIRM, and a turn on a decision whose snapshot is not null. They are columns and not members of the signed
--    document, so decision-record.v2 does not change (no contract change; the Writer holds the pair and the database keeps it).
-- 2. decision_record_input_state_key and decision_record_input_confirmed_key (0034, 0038) are rebuilt over COALESCE of the two columns. A decision with neither is keyed as before (the empty
--    text is not a UUID, so it never collides with a turn). Each index is built under a temporary name, the old one is dropped and the new one takes its name, so a violation is still reported
--    against the same constraint name. The state key is created before the confirmed key, as in 0034 and 0038, because the database checks the older index first: a violation of both (the
--    same request twice) is reported against the state key. If rows already hold a duplicate of the new keys the migration fails and a person must look; nothing is deleted (decision_record
--    is insert-only).

ALTER TABLE decision_record ADD COLUMN turn_id text, ADD COLUMN principal_id text;

ALTER TABLE decision_record ADD CONSTRAINT decision_record_early_key_shape CHECK (
    (turn_id IS NULL AND principal_id IS NULL)
    OR coalesce(
         turn_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
         AND principal_id ~ '^usr_[0-9A-Za-z]{2,32}$'
         AND outcome NOT IN ('ALLOW', 'CONFIRM')
         AND jsonb_typeof(json -> 'evidence_snapshot_id') = 'null', false));

CREATE UNIQUE INDEX decision_record_input_state_key_0040 ON decision_record
  (tenant_id, (json ->> 'intent_id'), intent_hash, (COALESCE(json ->> 'evidence_snapshot_id', '')), (COALESCE(json ->> 'policy_version', '')),
   (COALESCE(confirmation_request_id, '')), (COALESCE(turn_id, '')), (COALESCE(principal_id, '')));
CREATE UNIQUE INDEX decision_record_input_confirmed_key_0040 ON decision_record
  (tenant_id, (json ->> 'intent_id'), intent_hash, (COALESCE(json ->> 'evidence_snapshot_id', '')), (COALESCE(json ->> 'policy_version', '')), outcome,
   (COALESCE(turn_id, '')), (COALESCE(principal_id, ''))) WHERE confirmation_request_id IS NOT NULL;
DROP INDEX decision_record_input_state_key;
ALTER INDEX decision_record_input_state_key_0040 RENAME TO decision_record_input_state_key;
DROP INDEX decision_record_input_confirmed_key;
ALTER INDEX decision_record_input_confirmed_key_0040 RENAME TO decision_record_input_confirmed_key;
