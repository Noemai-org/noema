-- 0011: provision_emitter_login, the one way a login is bound to an emitter that is not a power.
-- EDG-01, DEBT-07 (docs#106 R16; the Architect's decision on Q-DEBT04-1, option b: a login per such emitter). Forward-only.
--
-- Migration 0004 gave tenant_login an optional emitter column and record_insert_t refuses any written_by that is not the one bound to the
-- calling login. This function makes the mapping row of such a login (power 'record_emitter'), after the deployment has created the login
-- role in the group record_emitter (deploy/compose/postgres/init.sh). It is callable only by the migrating role, like
-- provision_tenant_login. It checks that the role can log in, is a member of record_emitter and of no power or read/verify group, that
-- the emitter has the shape of one and is not the identity of a power (a power's own identity is bound to the power's logins), and that
-- the tenant exists (the table's foreign key). An emitter nobody provisions a login for, such as model-service and the adapters
-- (OD-P-73), simply cannot be written.
--
-- record_append owns the function (it must write tenant_login, which it owns); a migration that creates an object owned by record_append
-- holds CREATE on public for its own duration only (migrations README).

GRANT CREATE ON SCHEMA public TO record_append;

CREATE FUNCTION provision_emitter_login(p_login text, p_tenant text, p_emitter text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE g text;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles r WHERE r.rolname = p_login AND r.rolcanlogin) THEN
    RAISE EXCEPTION 'provision: % is not a login role: create it in deploy/compose/postgres/init.sh first', p_login;
  END IF;
  IF NOT pg_has_role(p_login, 'record_emitter', 'MEMBER') THEN
    RAISE EXCEPTION 'provision: % is not a member of record_emitter', p_login;
  END IF;
  FOREACH g IN ARRAY ARRAY['trust_rw', 'execution_rw', 'outcome_rw', 'evidence_rw', 'orchestrator_rw', 'record_append', 'record_read', 'record_verify'] LOOP
    IF pg_has_role(p_login, g, 'MEMBER') THEN
      RAISE EXCEPTION 'provision: % is a member of %; an emitter login is a member of record_emitter only', p_login, g;
    END IF;
  END LOOP;
  IF p_emitter !~ '^[a-z][a-z0-9-]*$' THEN
    RAISE EXCEPTION 'provision: % is not the shape of an emitter', p_emitter;
  END IF;
  IF p_emitter IN ('core-trust', 'core-execution', 'core-outcome', 'core-evidence', 'core-workflows') THEN
    RAISE EXCEPTION 'provision: % is the identity of a power; it is written by that power''s own logins', p_emitter;
  END IF;
  INSERT INTO tenant_login (login_role, power, tenant_id, emitter) VALUES (p_login, 'record_emitter', p_tenant, p_emitter);
END $$;
ALTER FUNCTION provision_emitter_login(text, text, text) OWNER TO record_append;
REVOKE ALL ON FUNCTION provision_emitter_login(text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION provision_emitter_login(text, text, text) TO CURRENT_USER;

REVOKE CREATE ON SCHEMA public FROM record_append;
