-- 0002_grant_spent_void.sql
--
-- PERD v1.18 section 38.2, REQ-EXE-004, INV-27, US-19 (DEC-2026-10-28-spent-grant-void): a decision_grant moves
-- ISSUED -> SPENT, VOID or EXPIRED, and SPENT -> VOID only for a confirmed REJECTED_STALE_VERSION refusal.
-- Every other change of a grant that is SPENT, VOID or EXPIRED stays refused.
--
-- A refusal is confirmed when the grant's receipt (inserted earlier in the same transaction by execution_rw, and immutable
-- once inserted):
--   (a) has execution_state REJECTED_STALE_VERSION,
--   (b) records the target's version conflict as its transport result: the part of raw_status before the first colon is
--       VERSION_CONFLICT_409_412 (the Ledger writes the transport class, then the raw code, separated by colons),
--   (c) carries no external acknowledgement (NULL or the empty string),
--   (d) is the grant's only receipt, and is the receipt of the attempt that grant_spend recorded.
-- A refused conditional write applies nothing (INV-27), so a confirmed refusal is the evidence that no write was applied.
-- Outcome's own read follows from the outbox event written in the same transaction; it is not a precondition.
--
-- Everything else in decision_grant_guard is as in 0001: no DELETE, only the status may change, and a status other than
-- ISSUED is final except for the one transition above. The triggers are not touched; the function behind the row trigger
-- is replaced. Forward-only: 0001 is not edited.

CREATE OR REPLACE FUNCTION decision_grant_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'DELETE on decision_grant is not allowed' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  IF (NEW.grant_id, NEW.tenant_id, NEW.decision_id, NEW.nonce, NEW.json, NEW.policy_id, NEW.policy_version, NEW.expires_at, NEW.sig_key_id, NEW.sig_value, NEW.created_at)
     IS DISTINCT FROM
     (OLD.grant_id, OLD.tenant_id, OLD.decision_id, OLD.nonce, OLD.json, OLD.policy_id, OLD.policy_version, OLD.expires_at, OLD.sig_key_id, OLD.sig_value, OLD.created_at) THEN
    RAISE EXCEPTION 'decision_grant: only the status may change' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  IF OLD.status <> 'ISSUED' AND NEW.status <> OLD.status THEN
    IF OLD.status = 'SPENT' AND NEW.status = 'VOID'
       AND (SELECT count(*) FROM execution_receipt WHERE grant_id = OLD.grant_id) = 1
       AND EXISTS (
         SELECT 1
           FROM execution_receipt r
           JOIN grant_spend gs ON gs.grant_id = r.grant_id AND gs.attempt = r.attempt
          WHERE r.grant_id = OLD.grant_id
            AND r.tenant_id = OLD.tenant_id
            AND r.execution_state = 'REJECTED_STALE_VERSION'
            AND split_part(r.raw_status, ':', 1) = 'VERSION_CONFLICT_409_412'
            AND COALESCE(r.external_ack, '') = ''
       ) THEN
      RETURN NEW;
    END IF;
    RAISE EXCEPTION 'decision_grant: % is final', OLD.status USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  RETURN NEW;
END $$;
