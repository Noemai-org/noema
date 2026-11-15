-- 0017: only a superuser clears a tenant's kill switch; no runtime login does (Founder decision D5 of 7 Nov 2026, DEC-2026-11-07-founder-decisions-block-d; ruling A71, DEC-2026-11-08-rulings-a41-onward; REC-05, REQ-AUD-005, INV-11, INV-12).
-- Forward-only: never edited after merge. The number 0017 follows the merge order the Founders set on 8 Nov 2026 (after 0016); a later merge order renumbers (A34).
--
-- Migration 0006 gave record_verify UPDATE on tenant.kill_switch so that the chain verifier can stop a tenant after a break. An UPDATE privilege on a column
-- cannot say "only to true", so a verifier that was fooled, or a credential of the verification login, could also turn a person's stop back off. The trigger below
-- refuses a true to false change by every login that is not a superuser, whatever role it belongs to and whatever it has been granted (review F1 on noema#265:
-- a rule that named only record_verify would let any other role, given the column by a later grant, clear the switch). The database cannot tell a person from a
-- process; a person clears the switch through the administrator (a superuser, the break-glass path) until the control plane's ops login of EXE-06 exists, and
-- EXE-06 then adds that one login to this rule in its own migration (ruling A71: the clear needs C-ADMIN and writes a SECURITY_EVENT naming the actor).
-- What is tested about the superuser: no login role the deployment creates for a service is a superuser (kill_switch_test.go, init.sh). The superuser is the
-- operator's own PostgreSQL credential, held by no service; that it is held by a person only is an operating rule (runbook), not something the database can check.
--
-- Not SECURITY DEFINER: it reads only pg_roles, which every login can read, and it must run as the caller to see session_user. Setting the switch (false to true,
-- or true to true) is not a clear and stays allowed to whoever holds the privilege.
-- ENABLE ALWAYS so that session_replication_role = replica does not switch it off for a session.

CREATE FUNCTION public.tenant_kill_switch_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  IF OLD.kill_switch AND NOT NEW.kill_switch
     AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles r WHERE r.rolname = session_user AND r.rolsuper) THEN
    RAISE EXCEPTION 'tenant: only a person clears the kill switch (a superuser; the control plane ops login of EXE-06 is added by its own migration)' USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN NEW;
END $$;
ALTER FUNCTION public.tenant_kill_switch_guard() OWNER TO noema_migrator;
REVOKE ALL ON FUNCTION public.tenant_kill_switch_guard() FROM PUBLIC;

CREATE TRIGGER tenant_kill_switch_guard BEFORE UPDATE OF kill_switch ON public.tenant FOR EACH ROW EXECUTE FUNCTION public.tenant_kill_switch_guard();
ALTER TABLE public.tenant ENABLE ALWAYS TRIGGER tenant_kill_switch_guard;
