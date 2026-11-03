-- 0006: the verifier's privileges and the provisioning of tenant logins (DEBT-04 option A, slice 3).
-- REQ-AUD-005 (verification), REQ-SEC-004, INV-12, REQ-AUD-001. Forward-only: never edited after merge (AGENTS.md section 7).
--
-- No role is created here: the roles record_verify and the tenant logins are created by deploy/compose/postgres/init.sh, and
-- this file only grants to them. A missing role stops the migration with a message that says where to create it.

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'record_verify') THEN
    RAISE EXCEPTION 'the role record_verify does not exist: create it in deploy/compose/postgres/init.sh before migrating';
  END IF;
END $$;

-- The verification job (REC-05) reads every tenant's chain to recompute it, and stops a tenant's writes on a proven break. It
-- is not a power: it holds SELECT on the Record, the chain heads and the tenant list, and UPDATE on the kill switch, and
-- nothing else. It cannot insert, update or delete a Record row (the immutability triggers and the missing privileges).
GRANT USAGE ON SCHEMA public TO record_verify;
GRANT SELECT ON record_event, record_chain_head, tenant TO record_verify;
GRANT UPDATE (kill_switch) ON tenant TO record_verify;

-- Adds the mapping row of one tenant login, after the deployment has created the login role. Callable only by the role that
-- owns the deployment (the migrating role, noema_core in Compose): no power, no tenant login and no PUBLIC. It checks that the
-- login exists and is a member of the group role of its power, so a typo or a login of another power cannot be mapped.
CREATE FUNCTION provision_tenant_login(p_login text, p_power text, p_tenant text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE v_group text;
BEGIN
  v_group := CASE p_power WHEN 'record_read' THEN 'record_read' ELSE p_power || '_rw' END;
  IF NOT EXISTS (SELECT 1 FROM pg_roles r WHERE r.rolname = p_login AND r.rolcanlogin) THEN
    RAISE EXCEPTION 'provision: % is not a login role: create it in deploy/compose/postgres/init.sh first', p_login;
  END IF;
  IF NOT pg_has_role(p_login, v_group, 'MEMBER') THEN
    RAISE EXCEPTION 'provision: % is not a member of %', p_login, v_group;
  END IF;
  INSERT INTO tenant_login (login_role, power, tenant_id) VALUES (p_login, p_power, p_tenant);
END $$;
ALTER FUNCTION provision_tenant_login(text, text, text) OWNER TO record_append;
REVOKE ALL ON FUNCTION provision_tenant_login(text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION provision_tenant_login(text, text, text) TO CURRENT_USER;
