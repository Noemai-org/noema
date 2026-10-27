-- 0001: the Execution and Record tables of PERD 38.2, with per-power roles and immutability triggers.
-- Task PLT-06 (slice for the first end-to-end case, ENV-03); REQ-DATA-001, REQ-AUD-001, REQ-EXE-001, REQ-EXE-002.
-- Forward-only: this file is never edited after merge (AGENTS.md section 7). Later tables arrive in later files.
--
-- Not in this file, and why (see services/core/migrations/README.md): the Trust, Understanding, Evidence and
-- Outcome tables that Execution only reads (intent_object, decision_record, plan, context_snapshot, ...). Their
-- foreign keys are added by the migrations that create them.

-- Per-power roles (PERD 38.3, REQ-DATA-001). NOLOGIN: the deployment maps its login roles to these.
DO $$
DECLARE r text;
BEGIN
  FOREACH r IN ARRAY ARRAY['trust_rw', 'execution_rw', 'outcome_rw', 'record_append', 'evidence_rw', 'orchestrator_rw'] LOOP
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
      EXECUTE format('CREATE ROLE %I NOLOGIN', r);
    END IF;
  END LOOP;
END $$;

-- The record functions below are owned by record_append, so the migrating role must be able to hand them over.
GRANT record_append TO CURRENT_USER;

-- Triggers --------------------------------------------------------------------------------------------------

CREATE FUNCTION reject_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION '% on % is not allowed: the table is insert-only', TG_OP, TG_TABLE_NAME USING ERRCODE = 'integrity_constraint_violation';
END $$;

-- tenant ----------------------------------------------------------------------------------------------------

CREATE TABLE tenant (
  tenant_id   text PRIMARY KEY,
  name        text NOT NULL,
  kill_switch boolean NOT NULL DEFAULT false,
  timezone    text NOT NULL DEFAULT 'UTC',
  created_at  timestamptz NOT NULL DEFAULT now()
);

-- decision_grant (written by Trust; Execution may move its status) -------------------------------------------

CREATE TABLE decision_grant (
  grant_id       text PRIMARY KEY,
  tenant_id      text NOT NULL REFERENCES tenant (tenant_id),
  decision_id    text NOT NULL,
  nonce          text NOT NULL,
  json           jsonb NOT NULL,
  status         text NOT NULL DEFAULT 'ISSUED' CHECK (status IN ('ISSUED', 'SPENT', 'VOID', 'EXPIRED')),
  policy_id      text NOT NULL,
  policy_version text NOT NULL,
  expires_at     timestamptz NOT NULL,
  sig_key_id     text NOT NULL,
  sig_value      text NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX decision_grant_tenant_status_idx ON decision_grant (tenant_id, status);

CREATE FUNCTION decision_grant_guard() RETURNS trigger LANGUAGE plpgsql AS $$
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
    RAISE EXCEPTION 'decision_grant: % is final', OLD.status USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER decision_grant_guard BEFORE UPDATE OR DELETE ON decision_grant FOR EACH ROW EXECUTE FUNCTION decision_grant_guard();
CREATE TRIGGER decision_grant_no_truncate BEFORE TRUNCATE ON decision_grant FOR EACH STATEMENT EXECUTE FUNCTION reject_mutation();

-- grant_spend (single use is a unique constraint, never workflow history: INV-30, REQ-EXE-001) ----------------

CREATE TABLE grant_spend (
  grant_id  text PRIMARY KEY REFERENCES decision_grant (grant_id),
  tenant_id text NOT NULL REFERENCES tenant (tenant_id),
  nonce     text NOT NULL,
  spent_at  timestamptz NOT NULL,
  CONSTRAINT grant_spend_nonce_key UNIQUE (nonce)
);
CREATE TRIGGER grant_spend_immutable BEFORE UPDATE OR DELETE ON grant_spend FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER grant_spend_no_truncate BEFORE TRUNCATE ON grant_spend FOR EACH STATEMENT EXECUTE FUNCTION reject_mutation();

-- idempotency_record (the key is committed before the call: REQ-EXE-002) --------------------------------------

CREATE TABLE idempotency_record (
  tenant_id       text NOT NULL REFERENCES tenant (tenant_id),
  idempotency_key text NOT NULL,
  logical_action  text NOT NULL,
  params_hash     text NOT NULL,
  attempt         bigint NOT NULL CHECK (attempt >= 1),
  status          text NOT NULL CHECK (status IN ('DISPATCHING', 'RECEIPTED')),
  external_ref    text,
  last_grant_id   text NOT NULL REFERENCES grant_spend (grant_id),
  created_at      timestamptz NOT NULL,
  updated_at      timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, idempotency_key)
);
CREATE INDEX idempotency_record_dispatching_idx ON idempotency_record (tenant_id, status) WHERE status = 'DISPATCHING';

CREATE FUNCTION idempotency_record_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'DELETE on idempotency_record is not allowed' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  IF (NEW.tenant_id, NEW.idempotency_key, NEW.logical_action, NEW.params_hash, NEW.created_at)
     IS DISTINCT FROM (OLD.tenant_id, OLD.idempotency_key, OLD.logical_action, OLD.params_hash, OLD.created_at) THEN
    RAISE EXCEPTION 'idempotency_record: the key, action and parameters never change' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  IF NEW.attempt < OLD.attempt OR (NEW.attempt = OLD.attempt AND OLD.status = 'RECEIPTED' AND NEW.status = 'DISPATCHING') THEN
    RAISE EXCEPTION 'idempotency_record: the attempt only moves forward' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER idempotency_record_guard BEFORE UPDATE OR DELETE ON idempotency_record FOR EACH ROW EXECUTE FUNCTION idempotency_record_guard();
CREATE TRIGGER idempotency_record_no_truncate BEFORE TRUNCATE ON idempotency_record FOR EACH STATEMENT EXECUTE FUNCTION reject_mutation();

-- execution_receipt (one per grant and attempt; the first writer wins) ----------------------------------------

CREATE TABLE execution_receipt (
  receipt_id      text PRIMARY KEY,
  tenant_id       text NOT NULL REFERENCES tenant (tenant_id),
  grant_id        text NOT NULL REFERENCES grant_spend (grant_id),
  attempt         bigint NOT NULL CHECK (attempt >= 1),
  idempotency_key text NOT NULL,
  logical_action  text NOT NULL,
  execution_state text NOT NULL CHECK (execution_state IN ('DISPATCHED', 'ACKNOWLEDGED', 'OUTCOME_UNKNOWN', 'REJECTED_BY_TARGET', 'REJECTED_STALE_VERSION', 'FAILED_TERMINAL')),
  raw_status      text NOT NULL,
  external_ack    text,
  dispatched_at   timestamptz NOT NULL,
  json            jsonb NOT NULL,
  sig_key_id      text NOT NULL,
  sig_value       text NOT NULL,
  created_at      timestamptz NOT NULL,
  CONSTRAINT execution_receipt_grant_attempt_key UNIQUE (grant_id, attempt)
);
CREATE INDEX execution_receipt_tenant_key_idx ON execution_receipt (tenant_id, idempotency_key);
CREATE TRIGGER execution_receipt_immutable BEFORE UPDATE OR DELETE ON execution_receipt FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER execution_receipt_no_truncate BEFORE TRUNCATE ON execution_receipt FOR EACH STATEMENT EXECUTE FUNCTION reject_mutation();

-- recovery_request (a void grant is addressed to Trust, once per grant) ---------------------------------------

CREATE TABLE recovery_request (
  recovery_request_id text PRIMARY KEY,
  tenant_id           text NOT NULL REFERENCES tenant (tenant_id),
  grant_id            text NOT NULL REFERENCES decision_grant (grant_id),
  reason              text NOT NULL,
  attempt_count       bigint NOT NULL DEFAULT 0,
  status              text NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN', 'DECIDED')),
  created_at          timestamptz NOT NULL,
  CONSTRAINT recovery_request_grant_key UNIQUE (grant_id)
);
CREATE INDEX recovery_request_open_idx ON recovery_request (tenant_id, status) WHERE status = 'OPEN';

-- outbox_event (an event commits with its state change or not at all: REQ-VER-005) ---------------------------

CREATE TABLE outbox_event (
  outbox_id     text PRIMARY KEY,
  tenant_id     text NOT NULL REFERENCES tenant (tenant_id),
  topic         text NOT NULL,
  payload       jsonb NOT NULL,
  created_at    timestamptz NOT NULL,
  dispatched_at timestamptz
);
CREATE INDEX outbox_event_pending_idx ON outbox_event (tenant_id, created_at) WHERE dispatched_at IS NULL;

-- The Record (REQ-AUD-001) ------------------------------------------------------------------------------------

CREATE TABLE record_event (
  event_id        text PRIMARY KEY,
  tenant_id       text NOT NULL REFERENCES tenant (tenant_id),
  seq             bigint NOT NULL CHECK (seq >= 1),
  prev_hash       text NOT NULL,
  hash            text NOT NULL,
  written_by      text NOT NULL,
  event_type      text NOT NULL,
  object_ref      text NOT NULL,
  payload_hash    text NOT NULL,
  correlation_id  text NOT NULL,
  at              timestamptz NOT NULL,
  schema_version  text NOT NULL,
  actor           text NOT NULL,
  body            jsonb NOT NULL,
  CONSTRAINT record_event_tenant_seq_key UNIQUE (tenant_id, seq),
  CONSTRAINT record_event_tenant_hash_key UNIQUE (tenant_id, hash)
);
CREATE INDEX record_event_correlation_idx ON record_event (tenant_id, correlation_id);
CREATE TRIGGER record_event_immutable BEFORE UPDATE OR DELETE ON record_event FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER record_event_no_truncate BEFORE TRUNCATE ON record_event FOR EACH STATEMENT EXECUTE FUNCTION reject_mutation();

CREATE TABLE record_chain_head (
  tenant_id text PRIMARY KEY REFERENCES tenant (tenant_id),
  seq       bigint NOT NULL,
  hash      text NOT NULL
);

-- No power touches the Record tables. They call these two functions, owned by record_append, which lock the head
-- row (one writer per tenant at a time) and refuse an event that does not extend the head they locked.
ALTER TABLE record_event OWNER TO record_append;
ALTER TABLE record_chain_head OWNER TO record_append;

CREATE FUNCTION record_lock_head(p_tenant text) RETURNS TABLE (seq bigint, hash text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
  INSERT INTO record_chain_head (tenant_id, seq, hash)
  VALUES (p_tenant, 0, encode(sha256(convert_to('noema:genesis:' || p_tenant, 'UTF8')), 'hex'))
  ON CONFLICT (tenant_id) DO NOTHING;
  RETURN QUERY SELECT h.seq, h.hash FROM record_chain_head h WHERE h.tenant_id = p_tenant FOR UPDATE;
END $$;

CREATE FUNCTION record_insert(
  p_tenant text, p_event_id text, p_seq bigint, p_prev_hash text, p_hash text, p_written_by text, p_event_type text,
  p_object_ref text, p_payload_hash text, p_correlation_id text, p_at timestamptz, p_schema_version text, p_actor text, p_body jsonb
) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE head record_chain_head%ROWTYPE;
BEGIN
  SELECT * INTO head FROM record_chain_head h WHERE h.tenant_id = p_tenant FOR UPDATE;
  IF NOT FOUND OR head.seq + 1 <> p_seq OR head.hash <> p_prev_hash THEN
    RAISE EXCEPTION 'record chain: the event does not extend the head of tenant %', p_tenant USING ERRCODE = 'serialization_failure';
  END IF;
  INSERT INTO record_event (event_id, tenant_id, seq, prev_hash, hash, written_by, event_type, object_ref, payload_hash, correlation_id, at, schema_version, actor, body)
  VALUES (p_event_id, p_tenant, p_seq, p_prev_hash, p_hash, p_written_by, p_event_type, p_object_ref, p_payload_hash, p_correlation_id, p_at, p_schema_version, p_actor, p_body);
  UPDATE record_chain_head SET seq = p_seq, hash = p_hash WHERE tenant_id = p_tenant;
END $$;

ALTER FUNCTION record_lock_head(text) OWNER TO record_append;
ALTER FUNCTION record_insert(text, text, bigint, text, text, text, text, text, text, text, timestamptz, text, text, jsonb) OWNER TO record_append;

-- Privileges --------------------------------------------------------------------------------------------------

REVOKE ALL ON ALL TABLES IN SCHEMA public FROM PUBLIC;
REVOKE ALL ON FUNCTION record_lock_head(text), record_insert(text, text, bigint, text, text, text, text, text, text, text, timestamptz, text, text, jsonb) FROM PUBLIC;

-- Every power that writes the Record may call the two functions, and nothing else on it.
GRANT EXECUTE ON FUNCTION record_lock_head(text), record_insert(text, text, bigint, text, text, text, text, text, text, text, timestamptz, text, text, jsonb)
  TO trust_rw, execution_rw, outcome_rw, evidence_rw, orchestrator_rw;
GRANT USAGE ON SCHEMA public TO trust_rw, execution_rw, outcome_rw, evidence_rw, orchestrator_rw, record_append;

-- Trust issues grants and decides recovery requests.
GRANT SELECT ON tenant, grant_spend, idempotency_record, execution_receipt TO trust_rw;
GRANT SELECT, INSERT ON decision_grant TO trust_rw;
GRANT SELECT ON recovery_request TO trust_rw;
GRANT UPDATE (status) ON recovery_request TO trust_rw;
GRANT SELECT, INSERT ON outbox_event TO trust_rw;

-- Execution spends and dispatches. It moves a grant's status and nothing else about a grant.
GRANT SELECT ON tenant TO execution_rw;
GRANT SELECT ON decision_grant TO execution_rw;
GRANT UPDATE (status) ON decision_grant TO execution_rw;
GRANT SELECT, INSERT ON grant_spend, execution_receipt, recovery_request TO execution_rw;
GRANT SELECT, INSERT, UPDATE (attempt, status, external_ref, last_grant_id, updated_at) ON idempotency_record TO execution_rw;
GRANT SELECT, INSERT ON outbox_event TO execution_rw;

-- Outcome reads receipts and enqueues its own work. It writes no receipt.
GRANT SELECT ON tenant, decision_grant, grant_spend, idempotency_record, execution_receipt, recovery_request TO outcome_rw;
GRANT SELECT, INSERT ON outbox_event TO outcome_rw;

-- The orchestrator's dispatcher marks outbox events dispatched.
GRANT SELECT ON tenant, outbox_event TO orchestrator_rw;
GRANT UPDATE (dispatched_at) ON outbox_event TO orchestrator_rw;

GRANT SELECT ON tenant TO evidence_rw;
