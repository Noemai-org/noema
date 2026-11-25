-- 0038: the basis of a Decision Record is three required keys, null for an outcome reached before the evaluation (TRU-05; rulings A574 and A577, interim, relayed and recorded in
-- DEC-2026-11-23-architect-rulings-a561-a580; PERD REQ-TRU-001, REQ-TRU-007, REQ-TRU-008; INV-03, INV-17, INV-21, INV-25). Number 0038 is the one the ledger on noema#183 reserved for A574.
-- Forward-only: never edited after merge. No CREATE ROLE here (TestNoMigrationAfter0001CreatesARole).
--
-- decision-record.v2 now lets evidence_snapshot_id, policy_version and evidence_freshness be null, and only for REJECT, CLARIFY, WAIT, ESCALATE and UNKNOWN: an outcome reached before the
-- evidence and the policy were evaluated claims no evaluation. ALLOW and CONFIRM keep all three, non-null, so neither a grant nor a request to confirm can rest on a missing basis. No outcome may
-- omit a key: the key is required and its value is null, never missing (A577). Three changes, all on decision_record:
--
-- 1. decision_record_bound_fields (0012, A13) required a non-empty string policy_version for every record. It is recreated without that clause; every other bound field is checked as before.
-- 2. decision_record_basis_by_outcome carries the rule above. For an ALLOW or a CONFIRM each member is present and non-null and of its type (a non-empty string, a non-empty string, an array); for any
--    other outcome each member is present and either null or of its type. jsonb_exists and jsonb_typeof, with coalesce, keep NULL from passing a CHECK.
-- 3. The two input-tuple unique indexes (0027, 0034; A172, A186; INV-21) are rebuilt over COALESCE of the snapshot and the policy version, because two NULLs are never equal in a unique index and a
--    decision with a null basis would otherwise be outside the key: a retried early decision could be stored twice. The empty text cannot be a real snapshot or policy version (the CHECK above
--    refuses it), so it never collides with one. Each index is built under a temporary name, the old one is dropped and the new one takes its name, so a violation is still reported against the
--    same constraint name. If rows already hold a duplicate of the new key the migration fails and a person must look at the database; nothing is deleted (decision_record is insert-only).
--
-- The immutability triggers of decision_record (0012), its grants, and its other constraints are not touched: only constraints and indexes change and no row is written, so no UPDATE trigger fires.
-- The Writer is not changed here: it still records a decision with a complete basis, and an early-gate outcome is recorded only by a later change (A578).

ALTER TABLE decision_record DROP CONSTRAINT decision_record_bound_fields;
ALTER TABLE decision_record ADD CONSTRAINT decision_record_bound_fields CHECK (coalesce(
    jsonb_typeof(json -> 'intent_id') IS NOT DISTINCT FROM 'string' AND coalesce(json ->> 'intent_id', '') <> ''
    AND jsonb_typeof(json -> 'correlation_id') IS NOT DISTINCT FROM 'string' AND coalesce(json ->> 'correlation_id', '') <> ''
    AND jsonb_typeof(json -> 'intent_hash') IS NOT DISTINCT FROM 'string' AND coalesce(json ->> 'intent_hash', '') <> ''
    AND CASE WHEN jsonb_typeof(json -> 'issued_at') IS NOT DISTINCT FROM 'string' AND jsonb_typeof(json -> 'expires_at') IS NOT DISTINCT FROM 'string'
              AND json ->> 'issued_at' ~ '^[0-9]{4}-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](\.[0-9]{1,9})?Z$' AND json ->> 'expires_at' ~ '^[0-9]{4}-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](\.[0-9]{1,9})?Z$'
             THEN (json ->> 'expires_at')::timestamptz > (json ->> 'issued_at')::timestamptz ELSE false END, false));

ALTER TABLE decision_record ADD CONSTRAINT decision_record_basis_by_outcome CHECK (coalesce(
    jsonb_exists(json, 'evidence_snapshot_id') AND jsonb_exists(json, 'policy_version') AND jsonb_exists(json, 'evidence_freshness')
    AND CASE WHEN outcome IN ('ALLOW', 'CONFIRM') THEN
           jsonb_typeof(json -> 'evidence_snapshot_id') IS NOT DISTINCT FROM 'string' AND coalesce(json ->> 'evidence_snapshot_id', '') <> ''
           AND jsonb_typeof(json -> 'policy_version') IS NOT DISTINCT FROM 'string' AND coalesce(json ->> 'policy_version', '') <> ''
           AND jsonb_typeof(json -> 'evidence_freshness') IS NOT DISTINCT FROM 'array'
         ELSE
           (jsonb_typeof(json -> 'evidence_snapshot_id') = 'null' OR (jsonb_typeof(json -> 'evidence_snapshot_id') = 'string' AND json ->> 'evidence_snapshot_id' <> ''))
           AND (jsonb_typeof(json -> 'policy_version') = 'null' OR (jsonb_typeof(json -> 'policy_version') = 'string' AND json ->> 'policy_version' <> ''))
           AND jsonb_typeof(json -> 'evidence_freshness') IN ('null', 'array')
         END, false));

CREATE UNIQUE INDEX decision_record_input_state_key_0038 ON decision_record
  (tenant_id, (json ->> 'intent_id'), intent_hash, (COALESCE(json ->> 'evidence_snapshot_id', '')), (COALESCE(json ->> 'policy_version', '')), (COALESCE(confirmation_request_id, '')));
DROP INDEX decision_record_input_state_key;
ALTER INDEX decision_record_input_state_key_0038 RENAME TO decision_record_input_state_key;

CREATE UNIQUE INDEX decision_record_input_confirmed_key_0038 ON decision_record
  (tenant_id, (json ->> 'intent_id'), intent_hash, (COALESCE(json ->> 'evidence_snapshot_id', '')), (COALESCE(json ->> 'policy_version', '')), outcome) WHERE confirmation_request_id IS NOT NULL;
DROP INDEX decision_record_input_confirmed_key;
ALTER INDEX decision_record_input_confirmed_key_0038 RENAME TO decision_record_input_confirmed_key;
