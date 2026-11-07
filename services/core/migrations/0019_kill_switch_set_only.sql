-- 0019: the verification login sets a tenant's kill switch and cannot clear it (Founder decision D5 of 7 Nov 2026; REC-05, REQ-AUD-005, INV-11, INV-12).
-- Forward-only: never edited after merge. The number is the next free one after D12's 0018; a later merge order renumbers (A34).
--
-- Migration 0006 gave record_verify UPDATE on tenant.kill_switch so that the chain verifier can stop a tenant after a break. An UPDATE privilege on a column
-- cannot say "only to true", so a verifier that was fooled, or a credential of the verification login, could also turn a person's stop back off. The trigger below
-- refuses a true to false change by a login that is a member of record_verify, unless that login is a superuser. A person clears the switch with the
-- administrator or the control plane's ops login (EXE-06), neither of which is a member of record_verify.
--
-- Not SECURITY DEFINER: it reads only pg_roles, which every login can read, and it must run as the caller to see session_user's membership.
-- ENABLE ALWAYS so that session_replication_role = replica does not switch it off for a session.

CREATE FUNCTION public.tenant_kill_switch_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  IF OLD.kill_switch AND NOT NEW.kill_switch
     AND pg_catalog.pg_has_role(session_user, 'record_verify', 'MEMBER')
     AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles r WHERE r.rolname = session_user AND r.rolsuper) THEN
    RAISE EXCEPTION 'tenant: the verification login may set the kill switch and only a person clears it' USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN NEW;
END $$;
ALTER FUNCTION public.tenant_kill_switch_guard() OWNER TO noema_migrator;
REVOKE ALL ON FUNCTION public.tenant_kill_switch_guard() FROM PUBLIC;

CREATE TRIGGER tenant_kill_switch_guard BEFORE UPDATE OF kill_switch ON public.tenant FOR EACH ROW EXECUTE FUNCTION public.tenant_kill_switch_guard();
ALTER TABLE public.tenant ENABLE ALWAYS TRIGGER tenant_kill_switch_guard;
