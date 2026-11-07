-- 0015: the policy versions a tenant has published (TRU-08 slice 1). REQ-TRU-005, REQ-OPS-003; INV-09, INV-21; ruling D1.
-- Number 0015 by Founder decision D12 (0012 #168, 0013 #229, 0014 #233, 0015 #259); if merge order changes, the later PR renumbers (ruling A34).
-- Forward-only: never edited after merge. No CREATE ROLE (TestNoMigrationAfter0001CreatesARole).
--
-- A publication is one row: (tenant, policy_id, version). The active version of a policy_id is the highest one published for it. Versions are numbers
-- of two or three components, compared as numbers and only within one policy_id. A version that is not higher than the active one (the same, or a
-- downgrade) is refused by a trigger, so no caller can make an older policy active again by publishing it.

-- The numeric key of a dotted version: three integers, the third 0 when absent. 1.10 > 1.9; 01.010 is 1.10; 3.1 and 3.1.0 are the same version.
CREATE FUNCTION policy_version_key(v text) RETURNS integer[]
LANGUAGE sql IMMUTABLE STRICT SET search_path = pg_catalog, pg_temp AS $$
  SELECT ARRAY[p[1]::integer, p[2]::integer, COALESCE(p[3], '0')::integer] FROM (SELECT string_to_array(v, '.') AS p) s
$$;

CREATE TABLE policy_publication (
  tenant_id     text NOT NULL REFERENCES tenant (tenant_id),
  policy_id     text NOT NULL CHECK (policy_id ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$'),
  version       text NOT NULL CHECK (version ~ '^[0-9]+\.[0-9]+(\.[0-9]+)?$'),
  version_key   integer[] NOT NULL,
  published_by  text NOT NULL,
  published_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, policy_id, version_key),
  CONSTRAINT policy_publication_key_is_the_version CHECK (version_key = policy_version_key(version))
);
-- the active version of a policy_id: the highest key
CREATE INDEX policy_publication_active_idx ON policy_publication (tenant_id, policy_id, version_key DESC);

CREATE FUNCTION policy_publication_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
DECLARE newest integer[];
BEGIN
  -- the writer is a Trust login mapped to this tenant (DEBT-04 option A: session_user, schema-qualified, pg_temp last)
  IF NOT EXISTS (SELECT 1 FROM public.tenant_login l
                 WHERE l.login_role = session_user::text AND l.power = 'trust' AND l.tenant_id = NEW.tenant_id) THEN
    RAISE EXCEPTION 'policy_publication: this login may not publish for tenant %', NEW.tenant_id USING ERRCODE = 'insufficient_privilege';
  END IF;
  -- serialise the publications of one policy so that "higher than the active one" is decided on a stable view
  PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id || '/' || NEW.policy_id, 0));
  SELECT max(p.version_key) INTO newest FROM public.policy_publication p WHERE p.tenant_id = NEW.tenant_id AND p.policy_id = NEW.policy_id;
  IF newest IS NOT NULL AND NEW.version_key <= newest THEN
    RAISE EXCEPTION 'policy_publication: version % of % is not higher than the active one', NEW.version, NEW.policy_id USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION policy_publication_guard() FROM PUBLIC;
CREATE TRIGGER policy_publication_guard BEFORE INSERT ON policy_publication FOR EACH ROW EXECUTE FUNCTION policy_publication_guard();

-- Insert-only, like the Record: a publication is never edited, deleted or truncated, by any role.
CREATE TRIGGER policy_publication_immutable BEFORE UPDATE OR DELETE ON policy_publication FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER policy_publication_no_truncate BEFORE TRUNCATE ON policy_publication FOR EACH STATEMENT EXECUTE FUNCTION reject_mutation();

REVOKE ALL ON policy_publication FROM PUBLIC;
GRANT SELECT, INSERT ON policy_publication TO trust_rw;   -- only Trust publishes
GRANT SELECT ON policy_publication TO execution_rw;       -- Execution reads the active version at E4

-- Reads are scoped to the login's tenant by row-level security on the same session_user mapping (ruling A6, as noema#167 and noema#168): a
-- table-wide SELECT would let any tenant's login read every tenant's policy versions. A session with no tenant mapping sees no row.
CREATE FUNCTION policy_login_tenant() RETURNS text
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
  SELECT l.tenant_id FROM public.tenant_login l WHERE l.login_role = session_user::text
$$;
REVOKE ALL ON FUNCTION policy_login_tenant() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION policy_login_tenant() TO trust_rw, execution_rw;
ALTER TABLE policy_publication ENABLE ROW LEVEL SECURITY;
CREATE POLICY policy_publication_read ON policy_publication FOR SELECT TO trust_rw, execution_rw USING (tenant_id = policy_login_tenant());
CREATE POLICY policy_publication_write ON policy_publication FOR INSERT TO trust_rw WITH CHECK (tenant_id = policy_login_tenant());
-- policy_publication_guard is SECURITY DEFINER and reads this table as its owner, which FORCE subjects to the policies: without a policy for the owner it
-- would see no row, and every version would look like the first. CURRENT_USER is that owner here (the role that applies this file). The scope is
-- unchanged: policy_login_tenant() reads session_user, so the owner sees a tenant's rows only inside a call made by that tenant's login.
CREATE POLICY policy_publication_read_definer ON policy_publication FOR SELECT TO CURRENT_USER USING (tenant_id = policy_login_tenant());
-- the owner (administrator, migration role) is subject to the policies too, so it reads and writes no tenant's rows without a mapping
ALTER TABLE policy_publication FORCE ROW LEVEL SECURITY;
