-- 0011: the Decision Record, and the rule that a grant exists only for an ALLOW decision of the same tenant (TRU-05; PERD 38.2).
-- REQ-TRU-001, REQ-TRU-008, REQ-DATA-001; INV-03, INV-16. Number decided by the Architect on 4 Nov 2026 (after noema#136's 0007).
-- Forward-only: this file is never edited after merge. No CREATE ROLE here (TestNoMigrationAfter0001CreatesARole).

CREATE TABLE decision_record (
  decision_id             text PRIMARY KEY,
  tenant_id               text NOT NULL REFERENCES tenant (tenant_id),
  json                    jsonb NOT NULL,
  outcome                 text NOT NULL CHECK (outcome IN ('ALLOW', 'CLARIFY', 'CONFIRM', 'WAIT', 'ESCALATE', 'REJECT', 'UNKNOWN')),
  disposition_decision_id text,
  -- 'ESCALATE' whenever a disposition is named and null otherwise: it lets a plain foreign key require that the target's outcome is ESCALATE
  disposition_outcome     text GENERATED ALWAYS AS (CASE WHEN disposition_decision_id IS NOT NULL THEN 'ESCALATE' END) STORED,
  intent_hash             text NOT NULL CHECK (intent_hash ~ '^sha256:[0-9a-f]{64}$'),
  policy_versions_json    jsonb NOT NULL,
  sig_key_id              text NOT NULL,
  sig_value               text NOT NULL,
  created_at              timestamptz NOT NULL DEFAULT now(),
  -- The signed document is the Decision Record; the columns are what the grant guard and the foreign keys read. They must agree, or a
  -- document that says REJECT could be stored as ALLOW and authorise a grant.
  CONSTRAINT decision_record_tenant_decision_outcome_key UNIQUE (tenant_id, decision_id, outcome),
  -- the link names an ESCALATE decision of the same tenant (REQ-TRU-010, decision-record.v1; a key on the decision ID alone would accept any decision of any tenant)
  CONSTRAINT decision_record_disposition_fkey FOREIGN KEY (tenant_id, disposition_decision_id, disposition_outcome) REFERENCES decision_record (tenant_id, decision_id, outcome),
  CONSTRAINT decision_record_document_matches_columns CHECK (
    json ->> 'outcome' IS NOT DISTINCT FROM outcome AND json ->> 'decision_id' IS NOT DISTINCT FROM decision_id
    AND json ->> 'tenant_id' IS NOT DISTINCT FROM tenant_id)  -- IS NOT DISTINCT: a document without the field is refused, not passed as NULL
);
CREATE INDEX decision_record_tenant_outcome_idx ON decision_record (tenant_id, outcome);
CREATE INDEX decision_record_disposition_idx ON decision_record (disposition_decision_id) WHERE disposition_decision_id IS NOT NULL;

-- Insert-only, like the Record: a decision is never edited, deleted or truncated, by any role.
CREATE TRIGGER decision_record_immutable BEFORE UPDATE OR DELETE ON decision_record FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER decision_record_no_truncate BEFORE TRUNCATE ON decision_record FOR EACH STATEMENT EXECUTE FUNCTION reject_mutation();

REVOKE ALL ON decision_record FROM PUBLIC;
GRANT SELECT, INSERT ON decision_record TO trust_rw;   -- only Trust writes decisions
GRANT SELECT ON decision_record TO execution_rw;       -- Execution checks the decision a grant came from
GRANT SELECT ON decision_record TO outcome_rw;

-- A write is bound to the tenant of the login that makes it (DEBT-04 option A; REQ-SEC-004, INV-11), as for the Record and for evidence: session_user, which
-- a caller cannot change, is looked up in public.tenant_login (schema-qualified, with pg_temp last in the search path so a session's temporary table
-- cannot stand in for it). A Trust login with no mapping, a login of another power and a row naming another tenant are refused; a foreign key proves
-- only that a tenant exists. A superuser is not bound: it can disable any trigger, and no runtime login is one. Reads are scoped by row-level security below.
CREATE FUNCTION decision_record_tenant_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  IF (SELECT r.rolsuper FROM pg_catalog.pg_roles r WHERE r.rolname = session_user::text) THEN
    RETURN NEW;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM public.tenant_login l
                 WHERE l.login_role = session_user::text AND l.power = 'trust' AND l.tenant_id = NEW.tenant_id) THEN
    RAISE EXCEPTION 'decision_record: this login may not write decisions for tenant %', NEW.tenant_id USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION decision_record_tenant_guard() FROM PUBLIC;
CREATE TRIGGER decision_record_tenant_guard BEFORE INSERT ON decision_record FOR EACH ROW EXECUTE FUNCTION decision_record_tenant_guard();

-- Reads and writes are scoped to the login's tenant by row-level security on the same session_user mapping (as noema#167 does for evidence): a table-wide
-- SELECT would let any tenant's Trust, Execution or Outcome login read every tenant's decisions. A session with no tenant mapping sees no row. The table's
-- owner, and so the administrator and the migration role, are not subject to the policies (no FORCE), which is the audit and migration path.
CREATE FUNCTION decision_login_tenant() RETURNS text
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
  SELECT l.tenant_id FROM public.tenant_login l WHERE l.login_role = session_user::text
$$;
REVOKE ALL ON FUNCTION decision_login_tenant() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION decision_login_tenant() TO trust_rw, execution_rw, outcome_rw;
ALTER TABLE decision_record ENABLE ROW LEVEL SECURITY;
CREATE POLICY decision_record_read ON decision_record FOR SELECT TO trust_rw, execution_rw, outcome_rw USING (tenant_id = decision_login_tenant());
CREATE POLICY decision_record_write ON decision_record FOR INSERT TO trust_rw WITH CHECK (tenant_id = decision_login_tenant());
-- FORCE: the table's owner is subject to the policies too, so a migration or admin role that is not a superuser reads and writes no tenant's rows without
-- a tenant mapping (ruling A6). A superuser bypasses row security in PostgreSQL whatever is set; no runtime login is one.
ALTER TABLE decision_record FORCE ROW LEVEL SECURITY;

-- decision_grant gets the same scoping (ruling A6, DEBT-06 for this table): Trust writes and reads its own tenant's grants, Execution reads them and moves
-- their status, Outcome reads them. Foreign keys that point at the table (grant_spend, receipts) are checked without row security by PostgreSQL.
ALTER TABLE decision_grant ENABLE ROW LEVEL SECURITY;
CREATE POLICY decision_grant_read ON decision_grant FOR SELECT TO trust_rw, execution_rw, outcome_rw USING (tenant_id = decision_login_tenant());
CREATE POLICY decision_grant_write ON decision_grant FOR INSERT TO trust_rw WITH CHECK (tenant_id = decision_login_tenant());
CREATE POLICY decision_grant_status ON decision_grant FOR UPDATE TO execution_rw USING (tenant_id = decision_login_tenant()) WITH CHECK (tenant_id = decision_login_tenant());
ALTER TABLE decision_grant FORCE ROW LEVEL SECURITY;

-- The Decision Record comes first, and only an ALLOW decision of the grant's own tenant can have a grant (REQ-TRU-008: grants where the
-- decision is not ALLOW: 0). The trigger runs as the inserting role, which is Trust and holds SELECT on decision_record.
CREATE FUNCTION decision_grant_requires_allow() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE decided text;
BEGIN
  -- The signed grant document is what Execution decodes; the columns are what this guard reads. They must name the same decision and tenant.
  IF NEW.json ->> 'decision_id' IS DISTINCT FROM NEW.decision_id OR NEW.json ->> 'tenant_id' IS DISTINCT FROM NEW.tenant_id THEN
    RAISE EXCEPTION 'decision_grant: the signed grant names decision % of tenant %, its columns name % of %',
      NEW.json ->> 'decision_id', NEW.json ->> 'tenant_id', NEW.decision_id, NEW.tenant_id
      USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  SELECT d.outcome INTO decided FROM decision_record d WHERE d.decision_id = NEW.decision_id AND d.tenant_id = NEW.tenant_id;
  IF decided IS DISTINCT FROM 'ALLOW' THEN
    RAISE EXCEPTION 'decision_grant: decision % of tenant % is not a recorded ALLOW decision', NEW.decision_id, NEW.tenant_id
      USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER decision_grant_requires_allow BEFORE INSERT ON decision_grant FOR EACH ROW EXECUTE FUNCTION decision_grant_requires_allow();

-- outcome_record_verdict (migration 0007) is a SECURITY DEFINER function that reads decision_grant. Its search_path was pg_catalog, public: PostgreSQL searches the
-- session's temporary schema first for tables unless pg_temp is named, so a login with the TEMPORARY privilege could create pg_temp.decision_grant and have the
-- function read forged rows in place of the row-secured table. Pin pg_temp last (the same rule as noema#167). Only the setting changes, not the body.
DO $$
DECLARE f regprocedure;
BEGIN
  FOR f IN SELECT p.oid::regprocedure FROM pg_proc p WHERE p.proname = 'outcome_record_verdict' AND p.pronamespace = 'public'::regnamespace LOOP
    EXECUTE format('ALTER FUNCTION %s SET search_path = pg_catalog, public, pg_temp', f);
  END LOOP;
END $$;
