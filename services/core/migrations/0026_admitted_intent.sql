-- 0026: the admitted-intent store (ruling A151 of the Architect, 12 Nov 2026; REQ-INT-001, REQ-SEC-004, REQ-AUD-001; INV-11, INV-12, INV-17; A136).
-- Forward-only: this file is never edited after merge. No CREATE ROLE here (TestNoMigrationAfter0001CreatesARole).
-- The number is 0026 on C1's migration table (noema#183, ruling A167): 0022 stays reserved for the evidence-set binding of A119 (EVD-02, noema#410 closed unmerged),
-- 0023 is noema#472 (A153/A164), 0024 noema#478, 0025 noema#490. Nobody renumbers except on that table.
--
-- What it holds: every Intent Object the second schema gate admitted (understanding.Gate, status VALIDATED, identity overwritten from the session), one row per version, with
-- the hash the gate computed. Trust reads the intent by reference and Execution reads the latest hash of an investigation at E3 (noema#447, noema#448). Only the
-- orchestrator's login writes it, and it writes the Record event INTENT_ADMITTED first, in the same transaction (the store is services/core/understanding/intentstore).

CREATE TABLE admitted_intent (
  tenant_id      text NOT NULL REFERENCES tenant (tenant_id),
  intent_id      text NOT NULL CHECK (intent_id ~ '^int_[0-9A-Za-z_]{2,64}$'),
  version        integer NOT NULL CHECK (version >= 1),
  supersedes     text,
  -- the version this one supersedes, kept as a column so that a plain foreign key can require that it exists in the same tenant, for the same intent and investigation
  prev_version   integer GENERATED ALWAYS AS (CASE WHEN version > 1 THEN version - 1 END) STORED,
  intent_node    text NOT NULL CHECK (intent_node ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$'),
  intent_hash    text NOT NULL CHECK (intent_hash ~ '^sha256:[0-9a-f]{64}$'),
  correlation_id text NOT NULL CHECK (correlation_id ~ '^corr_[0-9A-Za-z]{2,32}$'),
  json           jsonb NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, intent_id, version),
  CONSTRAINT admitted_intent_chain_key UNIQUE (tenant_id, intent_id, version, correlation_id),
  -- one intent chain per investigation: two intents cannot both be version 1 of one correlation, so "the latest intent of an investigation" is the highest version
  CONSTRAINT admitted_intent_one_per_correlation UNIQUE (tenant_id, correlation_id, version),
  CONSTRAINT admitted_intent_previous_fkey FOREIGN KEY (tenant_id, intent_id, prev_version, correlation_id) REFERENCES admitted_intent (tenant_id, intent_id, version, correlation_id),
  -- version 1 supersedes nothing; version n names version n-1 of the same intent (intent-object.v1)
  CONSTRAINT admitted_intent_supersedes_form CHECK ((version = 1 AND supersedes IS NULL) OR (version > 1 AND supersedes = intent_id || '@' || (version - 1)::text)),
  -- The admitted document is what Trust and Execution decode; the columns are what the keys and the guard read. They must agree, and the document must be one the gate
  -- admitted (status VALIDATED). IS NOT DISTINCT FROM keeps NULL from passing a CHECK; a fractional version ("1.0") renders as '1.0' and never equals '1'.
  CONSTRAINT admitted_intent_document_matches_columns CHECK (
    json ->> 'intent_id' IS NOT DISTINCT FROM intent_id AND json ->> 'tenant_id' IS NOT DISTINCT FROM tenant_id AND json ->> 'correlation_id' IS NOT DISTINCT FROM correlation_id
    AND jsonb_typeof(json -> 'version') IS NOT DISTINCT FROM 'number' AND json ->> 'version' IS NOT DISTINCT FROM version::text
    AND json ->> 'supersedes' IS NOT DISTINCT FROM supersedes AND json ->> 'status' IS NOT DISTINCT FROM 'VALIDATED' AND json ->> 'intent_node' IS NOT DISTINCT FROM intent_node)
);
CREATE INDEX admitted_intent_correlation_idx ON admitted_intent (tenant_id, correlation_id, version DESC);

-- Insert-only, like the Record: an admitted intent is never edited, deleted or truncated, by any role. A newer understanding is a new version.
CREATE TRIGGER admitted_intent_immutable BEFORE UPDATE OR DELETE ON admitted_intent FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER admitted_intent_no_truncate BEFORE TRUNCATE ON admitted_intent FOR EACH STATEMENT EXECUTE FUNCTION reject_mutation();

REVOKE ALL ON admitted_intent FROM PUBLIC;
GRANT SELECT, INSERT ON admitted_intent TO orchestrator_rw;   -- the only writer (A151, Block D D10)
GRANT SELECT ON admitted_intent TO trust_rw, execution_rw;    -- Trust reads the intent it decides on; Execution reads its latest hash at E3

-- A write is bound to the tenant of the login that makes it, as for the Record and for decisions (DEBT-04 option A; REQ-SEC-004, INV-11): session_user, which a caller
-- cannot change, is looked up in public.tenant_login (schema-qualified, pg_temp last in the search path). A login that is not the orchestrator's login for NEW.tenant_id
-- is refused; a foreign key proves only that a tenant exists. A superuser is not bound (it can disable any trigger); no runtime login is one.
CREATE FUNCTION admitted_intent_tenant_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  IF (SELECT r.rolsuper FROM pg_catalog.pg_roles r WHERE r.rolname = session_user::text) THEN
    RETURN NEW;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM public.tenant_login l
                 WHERE l.login_role = session_user::text AND l.power = 'orchestrator' AND l.tenant_id = NEW.tenant_id) THEN
    RAISE EXCEPTION 'admitted_intent: this login may not write intents for tenant %', NEW.tenant_id USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION admitted_intent_tenant_guard() FROM PUBLIC;
CREATE TRIGGER admitted_intent_tenant_guard BEFORE INSERT ON admitted_intent FOR EACH ROW EXECUTE FUNCTION admitted_intent_tenant_guard();

-- Reads and writes are scoped to the login's tenant by row-level security on the same session_user mapping (decision_login_tenant(), migration 0012); a session with no
-- mapping sees no row. The orchestrator's login did not hold EXECUTE on that function until now, and its policies need it. RLS with FORCE in this table's own migration (A136).
GRANT EXECUTE ON FUNCTION decision_login_tenant() TO orchestrator_rw;
ALTER TABLE admitted_intent ENABLE ROW LEVEL SECURITY;
CREATE POLICY admitted_intent_read ON admitted_intent FOR SELECT TO orchestrator_rw, trust_rw, execution_rw USING (tenant_id = decision_login_tenant());
CREATE POLICY admitted_intent_write ON admitted_intent FOR INSERT TO orchestrator_rw WITH CHECK (tenant_id = decision_login_tenant());
ALTER TABLE admitted_intent FORCE ROW LEVEL SECURITY;
