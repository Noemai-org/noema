-- 0042: the control plane sets and clears a tenant's kill switch (EXE-06; REQ-EXE-008, REQ-OPS-002; INV-03, INV-11, INV-12; ruling A71 and the plan of 0017; A633 and A666, interim, relayed,
-- not yet committed). Forward-only: never edited after merge. Number 0042 follows the merge order of the ledger (0038 and 0039 merged, 0040 reserved for the early-decision key, 0041 is TRU-11).
--
-- Migration 0017 said: until the control plane's ops login exists a person clears the switch through the administrator, and EXE-06 adds that one login to the rule in its own migration.
-- This is that migration. The control plane's login is the tenant's admin emitter login that migration 0011 already provisions (power record_emitter, emitter `admin`): it holds the Record
-- append functions and, below, one function. No new role (a migration after 0001 creates none) and no new privilege on the tenant table for any login:
--
--   admin_login_tenant()      the tenant of an admin emitter login, by session_user (which a caller cannot change), or NULL for every other login. Callable by record_append only.
--   kill_switch_apply_t(on)   sets or clears the switch of the calling login's own tenant and returns the previous state. It takes no tenant argument. SECURITY DEFINER, owned by
--                             record_append, EXECUTE for record_emitter alone; a login of another emitter is refused inside (42501).
--   tenant_kill_switch_guard  the rule of 0017 gains one way through, and only one: a clear (true to false) made by a definer-owned statement of record_append (current_user) on behalf of
--                             a session_user that is the tenant's admin login. A superuser still clears (the person's break-glass path), and no other login can, whatever it is granted.
--
-- The database cannot tell a person from a process (0017 says so). The listener's authenticated role (C-ADMIN or C-VERIFY) and the Record event that names the person are the control
-- plane's job (execution/controlplane); this migration makes the admin emitter login the only runtime path that clears, and a function that does nothing else.
-- Row security: tenant forces it (0030), so record_append, the owner of the function, gets a SELECT and an UPDATE policy that match the admin login's own tenant only; a session with
-- no admin mapping, including record_append's own, sees and changes no tenant row.

GRANT CREATE ON SCHEMA public TO record_append;

CREATE FUNCTION public.admin_login_tenant() RETURNS text
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
  SELECT l.tenant_id FROM public.tenant_login l
   WHERE l.login_role = session_user::text AND l.power = 'record_emitter' AND l.emitter = 'admin'
$$;
ALTER FUNCTION public.admin_login_tenant() OWNER TO record_append;
REVOKE ALL ON FUNCTION public.admin_login_tenant() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.admin_login_tenant() TO record_append;

GRANT SELECT (tenant_id, kill_switch), UPDATE (kill_switch) ON public.tenant TO record_append;
CREATE POLICY tenant_control_plane_read ON public.tenant FOR SELECT TO record_append USING (tenant_id = public.admin_login_tenant());
CREATE POLICY tenant_control_plane_apply ON public.tenant FOR UPDATE TO record_append
  USING (tenant_id = public.admin_login_tenant()) WITH CHECK (tenant_id = public.admin_login_tenant());

CREATE FUNCTION public.kill_switch_apply_t(p_on boolean) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
DECLARE v_tenant text := public.admin_login_tenant(); v_prev boolean;
BEGIN
  IF v_tenant IS NULL THEN
    RAISE EXCEPTION 'kill switch: this login is not a tenant''s control-plane admin login' USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF p_on IS NULL THEN
    RAISE EXCEPTION 'kill switch: the new state is needed' USING ERRCODE = 'null_value_not_allowed';
  END IF;
  SELECT t.kill_switch INTO v_prev FROM public.tenant t WHERE t.tenant_id = v_tenant FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'kill switch: the tenant has no row' USING ERRCODE = 'no_data_found';
  END IF;
  UPDATE public.tenant SET kill_switch = p_on WHERE tenant_id = v_tenant;
  RETURN v_prev;
END $$;
ALTER FUNCTION public.kill_switch_apply_t(boolean) OWNER TO record_append;
REVOKE ALL ON FUNCTION public.kill_switch_apply_t(boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.kill_switch_apply_t(boolean) TO record_emitter;

-- The guard of 0017, with the one way through added. current_user is the owner inside a SECURITY DEFINER call, so the statement of kill_switch_apply_t runs as record_append; the
-- session_user is still the login, and admin_login_tenant() must name the row's tenant. Both must hold. Not SECURITY DEFINER itself (it must run as the caller to see current_user).
CREATE OR REPLACE FUNCTION public.tenant_kill_switch_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  IF OLD.kill_switch AND NOT NEW.kill_switch THEN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles r WHERE r.rolname = session_user AND r.rolsuper) THEN
      RETURN NEW;
    END IF;
    IF current_user = 'record_append' THEN
      IF public.admin_login_tenant() IS NOT DISTINCT FROM OLD.tenant_id THEN
        RETURN NEW;
      END IF;
    END IF;
    RAISE EXCEPTION 'tenant: only a person clears the kill switch (a superuser, or the tenant''s control-plane admin login through kill_switch_apply_t)' USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN NEW;
END $$;

REVOKE CREATE ON SCHEMA public FROM record_append;
