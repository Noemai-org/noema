-- mock-case schema, v2 (REQ-ENV-008). SYNTHETIC. Forward-only; 0001 is not edited.
--
-- Reconciliation with the pack's case entity (packs/fintech_v1.json; SoT section 15.3 success predicate):
--   created_by_intent  the intent that created the case; the predicate counts cases per (subject, intent);
--   epoch              the logical resolution epoch the case belongs to (PERD section 29, idempotency key);
--   status ROUTED      a case that has been routed once. OPEN to ROUTED is the only status change there is.
-- Nothing closes or resolves a case, and ROUTED never goes back to OPEN.

ALTER TABLE cases ADD COLUMN created_by_intent text NOT NULL DEFAULT '';
ALTER TABLE cases ADD COLUMN epoch bigint NOT NULL DEFAULT 0;
ALTER TABLE cases ALTER COLUMN created_by_intent DROP DEFAULT;
ALTER TABLE cases ALTER COLUMN epoch DROP DEFAULT;
-- NOT VALID: a row created before this migration has no intent. Every new row must carry one.
ALTER TABLE cases ADD CONSTRAINT cases_intent_present CHECK (created_by_intent <> '') NOT VALID;
ALTER TABLE cases ADD CONSTRAINT cases_epoch_range CHECK (epoch >= 0 AND epoch <= 9007199254740991);

ALTER TABLE cases DROP CONSTRAINT cases_status_check;
ALTER TABLE cases ADD CONSTRAINT cases_status_check CHECK (status IN ('OPEN', 'ROUTED'));

CREATE OR REPLACE FUNCTION cases_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'DELETE on cases is not offered by mock-case' USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NEW.case_id <> OLD.case_id OR NEW.seq <> OLD.seq OR NEW.tenant_id <> OLD.tenant_id
     OR NEW.reference <> OLD.reference OR NEW.principal_id <> OLD.principal_id
     OR NEW.transaction_ref <> OLD.transaction_ref OR NEW.reason_code <> OLD.reason_code
     OR NEW.created_by_intent <> OLD.created_by_intent OR NEW.epoch <> OLD.epoch
     OR NEW.priority <> OLD.priority
     OR NEW.idempotency_key <> OLD.idempotency_key OR NEW.request_hash <> OLD.request_hash
     OR NEW.created_at <> OLD.created_at THEN
    RAISE EXCEPTION 'only status (OPEN to ROUTED), owner_queue, version and updated_at of a case can change' USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NEW.status <> OLD.status AND NOT (OLD.status = 'OPEN' AND NEW.status = 'ROUTED') THEN
    RAISE EXCEPTION 'a case status can only move from OPEN to ROUTED' USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NEW.version <> OLD.version + 1 THEN
    RAISE EXCEPTION 'the version of a case advances by exactly one' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  RETURN NEW;
END $$;

GRANT UPDATE (status) ON cases TO mock_case_app;
