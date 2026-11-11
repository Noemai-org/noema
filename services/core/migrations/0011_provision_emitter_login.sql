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
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles r WHERE r.rolname = p_login AND r.rolcanlogin) THEN
    RAISE EXCEPTION 'provision: % is not a login role: create it in deploy/compose/postgres/init.sh first', p_login;
  END IF;
  IF NOT pg_has_role(p_login, 'record_emitter', 'MEMBER') THEN
    RAISE EXCEPTION 'provision: % is not a member of record_emitter', p_login;
  END IF;
  -- Nothing but record_emitter (Codex review of noema#181): the login is a direct member of that role and of no other role at all (no power,
  -- read or verify group, no predefined role such as pg_read_all_data, no role made later), record_emitter belongs to no role, and the
  -- login has no privileged attribute. A fixed list of forbidden groups would miss any role not on it.
  IF EXISTS (SELECT 1 FROM pg_auth_members m JOIN pg_roles r ON r.oid = m.roleid JOIN pg_roles u ON u.oid = m.member
             WHERE u.rolname = p_login AND r.rolname <> 'record_emitter') THEN
    RAISE EXCEPTION 'provision: % is a member of a role other than record_emitter; an emitter login is a member of record_emitter only', p_login;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_auth_members m JOIN pg_roles u ON u.oid = m.member WHERE u.rolname = 'record_emitter') THEN
    RAISE EXCEPTION 'provision: record_emitter is a member of another role';
  END IF;
  IF EXISTS (SELECT 1 FROM pg_roles r WHERE r.rolname = p_login AND (r.rolsuper OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication OR r.rolbypassrls)) THEN
    RAISE EXCEPTION 'provision: % has a privileged attribute', p_login;
  END IF;
  IF p_emitter !~ '^[a-z][a-z0-9-]*$' THEN
    RAISE EXCEPTION 'provision: % is not the shape of an emitter', p_emitter;
  END IF;
  IF p_emitter IN ('core-trust', 'core-execution', 'core-outcome', 'core-evidence', 'core-workflows') THEN
    RAISE EXCEPTION 'provision: % is the identity of a power; it is written by that power''s own logins', p_emitter;
  END IF;
  -- Must equal the RUNTIME-scope, non-power actors of record/chain/actors.json (a test enforces it).
  IF p_emitter NOT IN ('core-edge', 'core-record', 'model-service', 'admin', 'mock-ledger', 'mock-switch', 'mock-destination', 'mock-customer', 'mock-policy', 'mock-case', 'mock-clock') THEN
    RAISE EXCEPTION 'provision: % is not a runtime emitter', p_emitter;
  END IF;
  INSERT INTO public.tenant_login (login_role, power, tenant_id, emitter) VALUES (p_login, 'record_emitter', p_tenant, p_emitter);
END $$;
ALTER FUNCTION provision_emitter_login(text, text, text) OWNER TO record_append;
REVOKE ALL ON FUNCTION provision_emitter_login(text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION provision_emitter_login(text, text, text) TO CURRENT_USER;

REVOKE CREATE ON SCHEMA public FROM record_append;
