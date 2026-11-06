-- 0013_grant_spent_void.sql (noema#94, renumbered from 0002 under D3 after 0012, the receipt signature migration of EXE-08 slice B)
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
--   (g) the receipt row exists. Its signature was verified inside the database by 0012 (Ed25519 over the exact bytes in
--       signed_jcs, key from signing_key, not revoked) before the row could be stored, and receipt_insert is the only insert
--       path (DEBT-01, DEC-2026-11-01-receipt-and-record-signing). A receipt row that this function can read is therefore a
--       verified one; this migration does not re-check the signature's shape, because a shape check is not a verification.
-- raw_status (the class, then the raw status code, e.g. VERSION_CONFLICT_409_412:409:VERSION_MISMATCH) is a diagnostic and
-- decides nothing, but it must agree with the signed transport_result: its part before the first colon is the same class.
-- A contradiction makes the receipt ambiguous, and an ambiguous receipt does not void a grant.
-- The signature is verified by 0012 on insert, so this guard needs no signature condition of its own. It must run after 0012:
-- applied before it, the guard would trust rows no one had verified (this is why #94's 0002 was renumbered).
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
            AND split_part(r.raw_status, ':', 1) = 'VERSION_CONFLICT_409_412'
       ) THEN
      RETURN NEW;
    END IF;
    RAISE EXCEPTION 'decision_grant: % is final', OLD.status USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  RETURN NEW;
END $$;
