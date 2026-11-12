-- 0025: the investigation table (STM-01 slice 2). REQ-STM-001, REQ-SEC-004; INV-12, INV-30; SR's note 2 on noema#264.
-- Number 0025: the next free after 0024 (A165) on the ledger of noema#183, proposed to the Steward; if merge order changes, the later PR renumbers (A34).
-- Forward-only: never edited after merge. No CREATE ROLE (TestNoMigrationAfter0001CreatesARole).
--
-- One row per investigation: the state and version of the projection of PERD 28.2, which is a projection of the Record (the Record event INVESTIGATION_STATE_CHANGED is
-- appended in the same transaction that moves the row, by the store; the event is the authority, the row is the cache the machine reads). The row is unique per
-- (tenant, input_received_ref): a second Start for one recorded INPUT_RECEIVED finds the first and never starts another run (A161; noema#477).
--
-- The legality of an arrow stays in the Go machine (transitions.yaml); the database enforces what it can know without a copy of the table: the state is one of the
-- machine's (a test compares the two lists), a new row is RECEIVED at version 0, a row moves by exactly one version, and nothing but the state, the version and
-- the time ever changes. The orchestrator's own login (power 'orchestrator', Record emitter core-workflows) is the only role that touches the table; the tenant of a
-- row is the login's, by row-level security with FORCE on the same session_user mapping as migration 0018.

CREATE TABLE investigation (
  tenant_id          text NOT NULL REFERENCES tenant (tenant_id),
  investigation_id   text NOT NULL CHECK (investigation_id ~ '^inv_[0-9A-Za-z]{2,32}$'),
  input_received_ref text NOT NULL CHECK (input_received_ref ~ '^recev_[0-9A-Za-z]{2,32}$'),
  correlation_id     text NOT NULL CHECK (correlation_id ~ '^corr_[0-9A-Za-z]{2,32}$'),
  state              text NOT NULL CHECK (state IN ('RECEIVED', 'INTERPRETING', 'REJECTED_AT_GATE', 'AWAITING_CLARIFICATION', 'EVALUATING', 'AWAITING_CONFIRMATION', 'WAITING', 'PLANNED', 'EXECUTING', 'AWAITING_VERIFICATION', 'AWAITING_EXTERNAL_OUTCOME', 'RECOVERING', 'BLOCKED_UNKNOWN', 'ESCALATING', 'EXPLAINING', 'EXPLAINING_BLOCKED', 'COMPLETED', 'ESCALATED', 'REJECTED', 'DORMANT', 'EXPIRED')),
  state_version      bigint NOT NULL CHECK (state_version >= 0),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, investigation_id),
  CONSTRAINT investigation_one_per_input UNIQUE (tenant_id, input_received_ref)
);

CREATE FUNCTION investigation_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.state <> 'RECEIVED' OR NEW.state_version <> 0 THEN
      RAISE EXCEPTION 'investigation: a new investigation is RECEIVED at version 0' USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.state_version <> OLD.state_version + 1 THEN
    RAISE EXCEPTION 'investigation: a row moves by exactly one version (from % to %)', OLD.state_version, NEW.state_version USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION investigation_guard() FROM PUBLIC;
CREATE TRIGGER investigation_guard BEFORE INSERT OR UPDATE ON investigation FOR EACH ROW EXECUTE FUNCTION investigation_guard();
CREATE TRIGGER investigation_no_delete BEFORE DELETE ON investigation FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER investigation_no_truncate BEFORE TRUNCATE ON investigation FOR EACH STATEMENT EXECUTE FUNCTION reject_mutation();

REVOKE ALL ON investigation FROM PUBLIC;
GRANT SELECT, INSERT ON investigation TO orchestrator_rw;
GRANT UPDATE (state, state_version, updated_at) ON investigation TO orchestrator_rw;   -- the identity columns never change

ALTER TABLE investigation ENABLE ROW LEVEL SECURITY;
ALTER TABLE investigation FORCE ROW LEVEL SECURITY;
CREATE POLICY investigation_tenant ON investigation FOR ALL TO orchestrator_rw
  USING (tenant_id = decision_login_tenant()) WITH CHECK (tenant_id = decision_login_tenant());
