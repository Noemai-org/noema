-- mock-case schema, v1 (REQ-ENV-008). SYNTHETIC: a test double for an enterprise case system.
-- Forward-only: this file is never edited after merge; a change is a new numbered file.
-- Run by mock_case_admin, which owns every object. The service serves traffic as mock_case_app.

CREATE FUNCTION mock_case_forbid() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION '% on % is not offered by mock-case: the table is append-only', TG_OP, TG_TABLE_NAME
    USING ERRCODE = 'insufficient_privilege';
END $$;

-- Gapless counters. A create takes its counter row, so references number 1, 2, 3 with no hole even when a
-- concurrent create loses a race and rolls back.
CREATE TABLE counters (
  kind  text   PRIMARY KEY CHECK (kind IN ('case', 'complaint', 'evidence')),
  value bigint NOT NULL CHECK (value >= 0)
);
INSERT INTO counters (kind, value) VALUES ('case', 0), ('complaint', 0), ('evidence', 0);

CREATE TABLE cases (
  case_id         text        PRIMARY KEY,
  seq             bigint      NOT NULL,
  tenant_id       text        NOT NULL,
  reference       text        NOT NULL,
  principal_id    text        NOT NULL,
  transaction_ref text        NOT NULL,
  reason_code     text        NOT NULL,
  -- Only OPEN is reachable. No operation closes or resolves a case, and the database refuses it too.
  status          text        NOT NULL CHECK (status = 'OPEN'),
  owner_queue     text        NOT NULL,
  priority        text        NOT NULL,
  idempotency_key text        NOT NULL,
  request_hash    text        NOT NULL,
  version         bigint      NOT NULL CHECK (version >= 1),
  created_at      timestamptz NOT NULL,
  updated_at      timestamptz NOT NULL,
  CONSTRAINT cases_tenant_key_uq   UNIQUE (tenant_id, idempotency_key),
  CONSTRAINT cases_tenant_ref_uq   UNIQUE (tenant_id, reference),
  CONSTRAINT cases_seq_uq          UNIQUE (seq)
);
CREATE INDEX cases_by_principal   ON cases (tenant_id, principal_id, seq);
CREATE INDEX cases_by_transaction ON cases (tenant_id, principal_id, transaction_ref, seq);

-- A case row changes in exactly one way: its queue moves and its version advances by one. Priority, status,
-- reference, the idempotency key and every other column are fixed for life, so "downgrade priority" or
-- "close" cannot happen even by a direct UPDATE; nothing is ever deleted.
CREATE FUNCTION cases_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'DELETE on cases is not offered by mock-case' USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NEW.case_id <> OLD.case_id OR NEW.seq <> OLD.seq OR NEW.tenant_id <> OLD.tenant_id
     OR NEW.reference <> OLD.reference OR NEW.principal_id <> OLD.principal_id
     OR NEW.transaction_ref <> OLD.transaction_ref OR NEW.reason_code <> OLD.reason_code
     OR NEW.status <> OLD.status OR NEW.priority <> OLD.priority
     OR NEW.idempotency_key <> OLD.idempotency_key OR NEW.request_hash <> OLD.request_hash
     OR NEW.created_at <> OLD.created_at THEN
    RAISE EXCEPTION 'only owner_queue, version and updated_at of a case can change' USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NEW.version <> OLD.version + 1 THEN
    RAISE EXCEPTION 'the version of a case advances by exactly one' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER cases_guard BEFORE UPDATE OR DELETE ON cases FOR EACH ROW EXECUTE FUNCTION cases_guard();
CREATE TRIGGER cases_no_truncate BEFORE TRUNCATE ON cases FOR EACH STATEMENT EXECUTE FUNCTION mock_case_forbid();

CREATE TABLE complaints (
  complaint_ref     text        PRIMARY KEY,
  seq               bigint      NOT NULL,
  tenant_id         text        NOT NULL,
  case_id           text        NOT NULL REFERENCES cases (case_id),
  idempotency_key   text        NOT NULL,
  request_hash      text        NOT NULL,
  logged_at         timestamptz NOT NULL,
  -- SYNTHETIC regulatory clock: it starts when the reference is logged and no operation changes it.
  clock_started_at  timestamptz NOT NULL,
  CONSTRAINT complaints_tenant_key_uq UNIQUE (tenant_id, idempotency_key),
  CONSTRAINT complaints_seq_uq        UNIQUE (seq)
);
CREATE INDEX complaints_by_case ON complaints (tenant_id, case_id, seq);

CREATE TABLE evidence (
  attachment_id   text        PRIMARY KEY,
  seq             bigint      NOT NULL,
  tenant_id       text        NOT NULL,
  case_id         text        NOT NULL REFERENCES cases (case_id),
  payload         text        NOT NULL,
  payload_hash    text        NOT NULL,
  idempotency_key text        NOT NULL,
  request_hash    text        NOT NULL,
  attached_at     timestamptz NOT NULL,
  CONSTRAINT evidence_tenant_key_uq UNIQUE (tenant_id, idempotency_key),
  CONSTRAINT evidence_seq_uq        UNIQUE (seq)
);
CREATE INDEX evidence_by_case ON evidence (tenant_id, case_id, seq);

-- Every command and every injected fault, with scenario, seed and fault-plan ID (REQ-ENV-008, Audit).
CREATE TABLE command_log (
  seq                 bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  at                  timestamptz NOT NULL,
  scenario_id         text        NOT NULL,
  seed                bigint      NOT NULL,
  fault_plan_id       text        NOT NULL,
  operation           text        NOT NULL,
  call_no             bigint      NOT NULL CHECK (call_no >= 1),
  tenant_id           text,
  case_id             text,
  idempotency_key     text,
  correlation_id      text,
  request_hash        text,
  status_code         integer     NOT NULL,
  error_code          text,
  committed           boolean     NOT NULL,
  replay              boolean     NOT NULL,
  fault_index         integer,
  fault_effect        text,
  fault_params        jsonb,
  response_suppressed boolean     NOT NULL
);
CREATE INDEX command_log_by_fault ON command_log (seq) WHERE fault_effect IS NOT NULL;

CREATE TRIGGER complaints_append_only BEFORE UPDATE OR DELETE ON complaints FOR EACH ROW EXECUTE FUNCTION mock_case_forbid();
CREATE TRIGGER complaints_no_truncate BEFORE TRUNCATE ON complaints FOR EACH STATEMENT EXECUTE FUNCTION mock_case_forbid();
CREATE TRIGGER evidence_append_only BEFORE UPDATE OR DELETE ON evidence FOR EACH ROW EXECUTE FUNCTION mock_case_forbid();
CREATE TRIGGER evidence_no_truncate BEFORE TRUNCATE ON evidence FOR EACH STATEMENT EXECUTE FUNCTION mock_case_forbid();
CREATE TRIGGER command_log_append_only BEFORE UPDATE OR DELETE ON command_log FOR EACH ROW EXECUTE FUNCTION mock_case_forbid();
CREATE TRIGGER command_log_no_truncate BEFORE TRUNCATE ON command_log FOR EACH STATEMENT EXECUTE FUNCTION mock_case_forbid();

-- The serving role: read everything it needs, insert, and move a case's queue. Nothing else.
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM PUBLIC;
GRANT SELECT, INSERT ON cases, complaints, evidence, command_log TO mock_case_app;
GRANT UPDATE (owner_queue, version, updated_at) ON cases TO mock_case_app;
GRANT SELECT, UPDATE ON counters TO mock_case_app;
GRANT USAGE ON ALL SEQUENCES IN SCHEMA public TO mock_case_app;
