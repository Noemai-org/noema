-- 0002_grant_spent_void.sql
--
-- PERD v1.18 section 38.2, REQ-EXE-004, INV-27, US-19 (DEC-2026-10-28-spent-grant-void): a decision_grant moves
-- ISSUED -> SPENT, VOID or EXPIRED, and SPENT -> VOID only for a confirmed REJECTED_STALE_VERSION refusal.
-- Every other change of a grant that is SPENT, VOID or EXPIRED stays refused.
--
-- A refusal is confirmed from the grant's receipt, read from the signed receipt document (execution-receipt.v1, stored in
-- execution_receipt.json) and cross-checked against the row's own columns and against grant_spend. Execution inserts the
-- receipt earlier in the same transaction as this update; a receipt row never changes (execution_receipt_immutable).
-- All of these must hold, and each is also checked in the signed document:
--   (a) the grant has exactly one receipt;
--   (b) the receipt is of the same tenant as the grant and of the same tenant as the spend;
--   (c) it is the receipt of the attempt that grant_spend recorded, with the idempotency key that grant_spend recorded;
--   (d) execution_state is REJECTED_STALE_VERSION;
--   (e) transport_result, a typed member of the receipt contract, is VERSION_CONFLICT_409_412: the class that the case
--       adapter (adapters.ClassVersionConflict) and the controller (TransportVersionConflict) share, and that the adapter
--       gives only to the real target's version refusal (mock-case: 409 VERSION_MISMATCH on a route). The contract fixes
--       the shape of transport_result but no value set; that is a limitation of this check, not of the contract;
--   (f) there is no external acknowledgement: the member is present and null in the signed document, and null or empty in
--       the row;
--   (g) the signature member of the document equals the row's signature columns (key and value), the key is not empty and
--       the value is 64 lower-case hex characters: the shape of an HMAC-SHA-256 as crypto.Sign writes it and crypto.Verify
--       accepts it. Equal copies and a valid shape are not a verification of the HMAC; the database cannot verify it. The
--       Controller verifies the receipt's HMAC before it hands the receipt to the Ledger (EXE-04), and a receipt whose
--       signature does not verify is never written.
-- raw_status (the class, then the raw status code, e.g. VERSION_CONFLICT_409_412:409:VERSION_MISMATCH) is a diagnostic and
-- decides nothing, but it must agree with the signed transport_result: its part before the first colon is the same class.
-- A contradiction makes the receipt ambiguous, and an ambiguous receipt does not void a grant.
-- The database holds no key (PERD 41.1: the grant key is held by the core from the secrets manager), so it checks the shape
-- of the signature and not the HMAC. In the core, Controller.completeReceipt verifies the receipt's HMAC with
-- KeyRing.VerifyWith before Ledger.Complete writes it, for the receipt of a dispatch and for the recovery sweeper's receipt;
-- a failed verification writes nothing and the attempt stays DISPATCHING. The SoT names Outcome as a further checker of the
-- receipt's signature (SoT 13.1, the "Checks" row of the constitution table); Outcome is not built. Only execution_rw can
-- insert a receipt.
-- A refused conditional write applies nothing (INV-27), so a confirmed refusal is the evidence that no write was applied.
-- That guarantee comes from mock-case's OpenAPI contract (route response 409 VERSION_MISMATCH, "nothing applied"), not from a
-- real target's contract: Tushe defines no case API. It must be re-verified against any real target before production use.
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
           JOIN grant_spend gs ON gs.grant_id = r.grant_id
          WHERE r.grant_id = OLD.grant_id
            AND gs.tenant_id = OLD.tenant_id
            AND r.tenant_id = OLD.tenant_id
            AND r.attempt = gs.attempt
            AND r.idempotency_key = gs.idempotency_key
            AND r.execution_state = 'REJECTED_STALE_VERSION'
            AND COALESCE(r.external_ack, '') = ''
            AND r.json->>'tenant_id' = OLD.tenant_id
            AND r.json->>'grant_id' = OLD.grant_id
            AND CASE WHEN jsonb_typeof(r.json->'attempt') = 'number' THEN (r.json->>'attempt')::numeric END = gs.attempt
            AND r.json->>'idempotency_key' = gs.idempotency_key
            AND r.json->>'execution_state' = 'REJECTED_STALE_VERSION'
            AND r.json->>'transport_result' = 'VERSION_CONFLICT_409_412'
            AND jsonb_typeof(r.json->'external_ack') = 'null'
            AND r.json->'sig'->>'key_id' = r.sig_key_id
            AND r.json->'sig'->>'value' = r.sig_value
            AND r.sig_key_id <> ''
            AND r.sig_value ~ '^[0-9a-f]{64}$'
            AND split_part(r.raw_status, ':', 1) = 'VERSION_CONFLICT_409_412'
       ) THEN
      RETURN NEW;
    END IF;
    RAISE EXCEPTION 'decision_grant: % is final', OLD.status USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  RETURN NEW;
END $$;
