-- 0004: Outcome's verdict, its durable watch and its one write path.
-- Task OUT-01; REQ-VER-001, REQ-VER-003, REQ-VER-004, REQ-RCV-002; INV-04, INV-15, INV-28.
-- Numbering: 0002 is reserved for noema#94 and 0003 for the record_read migration. The migrator applies by name
-- and a gap is allowed (platform/db.Migrate); outcome/store tests a fresh database and an upgrade where 0002 arrives
-- after this file. Forward-only: never edited after merge (AGENTS.md section 7).

-- outcome_verdict (insert-only; PERD 29, SoT 43.4) -----------------------------------------------------------

CREATE TABLE outcome_verdict (
  verdict_id         text PRIMARY KEY,
  tenant_id          text NOT NULL REFERENCES tenant (tenant_id),
  correlation_id     text NOT NULL,
  receipt_id         text NOT NULL REFERENCES execution_receipt (receipt_id),
  verification_id    text NOT NULL,
  outcome_definition text NOT NULL,
  verdict            text NOT NULL CHECK (verdict IN ('ACHIEVED', 'NOT_ACHIEVED', 'PARTIALLY_ACHIEVED', 'PENDING_EXTERNAL', 'UNKNOWN', 'COMPENSATED')),
  reason             text NOT NULL CHECK (reason <> ''),
  predicate_result   boolean,
  evidence_reads     text[] NOT NULL,
  verified_at        timestamptz NOT NULL,
  json               jsonb NOT NULL,
  sig_key_id         text NOT NULL,
  sig_value          text NOT NULL,
  -- One verdict per verification: the outbox delivers outcome.verify at least once, and a replay must find this verdict, not
  -- store a second one with a second RecoveryRequest. A watch check is a new verification with its own ID.
  CONSTRAINT outcome_verdict_verification_key UNIQUE (tenant_id, verification_id),
  -- outcome-verdict.v1.json: ACHIEVED needs a true predicate and at least one read; NOT_ACHIEVED needs a false one.
  CONSTRAINT outcome_verdict_achieved_shape CHECK (verdict <> 'ACHIEVED' OR (predicate_result IS TRUE AND cardinality(evidence_reads) >= 1)),
  CONSTRAINT outcome_verdict_not_achieved_shape CHECK (verdict <> 'NOT_ACHIEVED' OR predicate_result IS FALSE)
);
CREATE INDEX outcome_verdict_receipt_idx ON outcome_verdict (tenant_id, receipt_id, verified_at);
CREATE TRIGGER outcome_verdict_immutable BEFORE UPDATE OR DELETE ON outcome_verdict FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER outcome_verdict_no_truncate BEFORE TRUNCATE ON outcome_verdict FOR EACH STATEMENT EXECUTE FUNCTION reject_mutation();

-- outcome_watch (a PENDING_EXTERNAL verdict has exactly one; the workflow that drives it is OUT-03) -----------

CREATE TABLE outcome_watch (
  watch_id      text PRIMARY KEY,
  tenant_id     text NOT NULL REFERENCES tenant (tenant_id),
  verdict_id    text NOT NULL UNIQUE REFERENCES outcome_verdict (verdict_id),
  next_check_at timestamptz NOT NULL,
  deadline_at   timestamptz NOT NULL,
  triggers      text[] NOT NULL,
  status        text NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN', 'CLOSED')),
  created_at    timestamptz NOT NULL,
  CONSTRAINT outcome_watch_first_check_before_deadline CHECK (next_check_at <= deadline_at)
);
CREATE INDEX outcome_watch_due_idx ON outcome_watch (tenant_id, next_check_at) WHERE status = 'OPEN';

CREATE FUNCTION outcome_watch_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'DELETE on outcome_watch is not allowed: a watch is closed, never removed' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  IF (NEW.watch_id, NEW.tenant_id, NEW.verdict_id, NEW.deadline_at, NEW.triggers, NEW.created_at)
     IS DISTINCT FROM (OLD.watch_id, OLD.tenant_id, OLD.verdict_id, OLD.deadline_at, OLD.triggers, OLD.created_at) THEN
    RAISE EXCEPTION 'outcome_watch: only status and next_check_at may change' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  IF OLD.status = 'CLOSED' AND NEW.status <> 'CLOSED' THEN
    RAISE EXCEPTION 'outcome_watch: CLOSED is final' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER outcome_watch_guard BEFORE UPDATE OR DELETE ON outcome_watch FOR EACH ROW EXECUTE FUNCTION outcome_watch_guard();
CREATE TRIGGER outcome_watch_no_truncate BEFORE TRUNCATE ON outcome_watch FOR EACH STATEMENT EXECUTE FUNCTION reject_mutation();

-- recovery_request: a request comes from a void grant (Execution) or from a verdict (Outcome), never both ----------

ALTER TABLE recovery_request ALTER COLUMN grant_id DROP NOT NULL;
ALTER TABLE recovery_request ADD COLUMN verdict_id text REFERENCES outcome_verdict (verdict_id);
ALTER TABLE recovery_request ADD CONSTRAINT recovery_request_one_trigger CHECK (num_nonnulls(grant_id, verdict_id) = 1);
ALTER TABLE recovery_request ADD CONSTRAINT recovery_request_verdict_key UNIQUE (verdict_id);

-- A verdict-sourced request answers a verdict of the same tenant that is not ACHIEVED (PERD 29.1, REQ-VER-003).
CREATE FUNCTION recovery_request_verdict_guard() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
  IF NEW.verdict_id IS NOT NULL AND NOT EXISTS (
       SELECT 1 FROM outcome_verdict v WHERE v.verdict_id = NEW.verdict_id AND v.tenant_id = NEW.tenant_id AND v.verdict <> 'ACHIEVED') THEN
    RAISE EXCEPTION 'recovery_request: % does not answer a non-ACHIEVED verdict of tenant %', NEW.verdict_id, NEW.tenant_id USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER recovery_request_verdict_guard BEFORE INSERT ON recovery_request FOR EACH ROW EXECUTE FUNCTION recovery_request_verdict_guard();

-- The shape of a stored verdict, checked at commit for every inserter, the migrating role included (REQ-VER-003:
-- non-ACHIEVED verdicts without a RecoveryRequest SHALL be 0; REQ-VER-004: PENDING_EXTERNAL without a watch SHALL be 0).
CREATE FUNCTION outcome_verdict_complete() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE has_request boolean; has_watch boolean;
BEGIN
  SELECT EXISTS (SELECT 1 FROM recovery_request r WHERE r.verdict_id = NEW.verdict_id) INTO has_request;
  SELECT EXISTS (SELECT 1 FROM outcome_watch w WHERE w.verdict_id = NEW.verdict_id) INTO has_watch;
  IF (NEW.verdict <> 'ACHIEVED') <> has_request THEN
    RAISE EXCEPTION 'outcome_verdict %: a verdict has a recovery request exactly when it is not ACHIEVED', NEW.verdict_id USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  IF (NEW.verdict = 'PENDING_EXTERNAL') <> has_watch THEN
    RAISE EXCEPTION 'outcome_verdict %: a verdict has a watch exactly when it is PENDING_EXTERNAL', NEW.verdict_id USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER outcome_verdict_complete AFTER INSERT ON outcome_verdict DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION outcome_verdict_complete();

-- The write path -----------------------------------------------------------------------------------------------
-- The only way to store a verdict, its RecoveryRequest and its watch. EXECUTE goes to outcome_rw alone; the role has
-- no INSERT, UPDATE or DELETE on these tables. The Record events of the verdict are appended by the caller in the
-- same transaction through record_insert, which outcome_rw may already call (the hash is computed outside the
-- database). Until Record events are signed per writer (REC-08, REQ-AUD-006), anyone holding the outcome_rw
-- credential can call this function: the function bounds what is written, not who wrote it.

CREATE FUNCTION outcome_record_verdict(
  p_tenant text, p_verdict_id text, p_receipt_id text, p_verification_id text, p_correlation_id text, p_outcome_definition text,
  p_verdict text, p_reason text, p_predicate_result boolean, p_evidence_reads text[], p_verified_at timestamptz,
  p_sig_key_id text, p_sig_value text, p_json jsonb,
  p_recovery_request_id text,
  p_watch_id text, p_next_check_at timestamptz, p_deadline_at timestamptz, p_triggers text[]
) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE v_attempt bigint; v_corr text;
BEGIN
  SELECT r.attempt, g.json->>'correlation_id' INTO v_attempt, v_corr
    FROM execution_receipt r JOIN decision_grant g ON g.grant_id = r.grant_id AND g.tenant_id = r.tenant_id
    WHERE r.receipt_id = p_receipt_id AND r.tenant_id = p_tenant;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'outcome_record_verdict: receipt % does not exist for tenant %', p_receipt_id, p_tenant USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  -- The verdict belongs to the receipt's own grant and says in its columns what its signed document says. This bounds a
  -- caller that is not Outcome; it does not prove who called (REC-08).
  IF v_corr IS DISTINCT FROM p_correlation_id THEN
    RAISE EXCEPTION 'outcome_record_verdict: the correlation id is not the one of the receipt''s grant' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  IF (p_json->>'verdict_id', p_json->>'tenant_id', p_json->>'receipt_id', p_json->>'verdict', p_json->>'correlation_id', p_json->>'outcome_definition')
     IS DISTINCT FROM (p_verdict_id, p_tenant, p_receipt_id, p_verdict, p_correlation_id, p_outcome_definition) THEN
    RAISE EXCEPTION 'outcome_record_verdict: the document and the columns disagree' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  INSERT INTO outcome_verdict (verdict_id, tenant_id, correlation_id, receipt_id, verification_id, outcome_definition, verdict, reason, predicate_result,
                               evidence_reads, verified_at, json, sig_key_id, sig_value)
  VALUES (p_verdict_id, p_tenant, p_correlation_id, p_receipt_id, p_verification_id, p_outcome_definition, p_verdict, p_reason, p_predicate_result,
          p_evidence_reads, p_verified_at, p_json, p_sig_key_id, p_sig_value);
  IF p_verdict <> 'ACHIEVED' THEN
    IF p_recovery_request_id IS NULL THEN
      RAISE EXCEPTION 'outcome_record_verdict: a % verdict needs its recovery request id', p_verdict USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    INSERT INTO recovery_request (recovery_request_id, tenant_id, grant_id, verdict_id, reason, attempt_count, created_at)
    VALUES (p_recovery_request_id, p_tenant, NULL, p_verdict_id, p_reason, v_attempt, p_verified_at);
  ELSIF p_recovery_request_id IS NOT NULL THEN
    RAISE EXCEPTION 'outcome_record_verdict: an ACHIEVED verdict has no recovery request' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  IF p_verdict = 'PENDING_EXTERNAL' THEN
    IF p_watch_id IS NULL OR p_next_check_at IS NULL OR p_deadline_at IS NULL OR p_triggers IS NULL THEN
      RAISE EXCEPTION 'outcome_record_verdict: a PENDING_EXTERNAL verdict needs a watch with a deadline' USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    INSERT INTO outcome_watch (watch_id, tenant_id, verdict_id, next_check_at, deadline_at, triggers, created_at)
    VALUES (p_watch_id, p_tenant, p_verdict_id, p_next_check_at, p_deadline_at, p_triggers, p_verified_at);
  ELSIF p_watch_id IS NOT NULL THEN
    RAISE EXCEPTION 'outcome_record_verdict: only a PENDING_EXTERNAL verdict has a watch' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
END $$;

-- Privileges ---------------------------------------------------------------------------------------------------

REVOKE ALL ON FUNCTION outcome_record_verdict(text, text, text, text, text, text, text, text, boolean, text[], timestamptz, text, text, jsonb, text, text, timestamptz, timestamptz, text[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION outcome_record_verdict(text, text, text, text, text, text, text, text, boolean, text[], timestamptz, text, text, jsonb, text, text, timestamptz, timestamptz, text[]) TO outcome_rw;

GRANT SELECT ON outcome_verdict, outcome_watch TO outcome_rw;
GRANT SELECT ON outcome_verdict, outcome_watch TO trust_rw;

-- recovery_request keeps the privileges 0001 gave: Execution inserts the requests of void grants. It cannot forge a
-- verdict-sourced request: the verdict_guard trigger refuses one against an ACHIEVED verdict or another tenant's, and the
-- unique constraint on verdict_id refuses a second request for a verdict that already has one (every non-ACHIEVED verdict
-- is stored with its request in one transaction).
