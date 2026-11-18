-- 0030: row-level security with FORCE on the tenant table (A273, A280; SEC-08, REQ-SEC-004; INV-11, INV-12; closes DEBT-43).
-- Number 0030: assigned to C1 on the ledger of noema#183 (0029, the investigation principal, is merged; 0031 is the outbox, someone else's).
-- Forward-only: never edited after merge. No CREATE ROLE (TestNoMigrationAfter0001CreatesARole); every role named below exists from deploy/compose/postgres/init.sh.
--
-- Since migration 0001 every power login (trust_rw, execution_rw, outcome_rw, evidence_rw, orchestrator_rw) holds SELECT on tenant and the table had no policy, so any
-- tenant's login could list every tenant's ID and name (the Security Reviewer's P2 on noema#658). The legitimate reads are narrow: Execution reads its own tenant's kill
-- switch at E6 and again inside the spend transaction at E8, Trust reads it at T3, and the chain verifier (REC-05) lists every tenant and sets the switch after a proven break.
-- Three policies, one per role group, and nothing else:
--
--   tenant_own_row      the five power groups and record_read, ALL: tenant_id = decision_login_tenant() (migration 0012; the tenant of the login that connected, by session_user, which no caller
--                       can change). Same shape and same function as the policies of 0018. FOR ALL, not FOR SELECT, so that the privilege is what limits the command, as on the tables of
--                       0018: a power role that a later grant gave UPDATE (kill_switch) still reaches the guard of 0017, which refuses a clear whoever asks (TestNoRuntimeLoginCanClearTheKillSwitchEvenWithTheGrant).
--                       A login with no tenant mapping gets NULL from the function and sees nothing.
--   tenant_verify_read  record_verify, SELECT USING (true): the verification job lists every tenant (verify.go tenants()). It holds no write privilege except the column below.
--   tenant_verify_halt  record_verify, UPDATE USING (true) WITH CHECK (kill_switch): the job may only produce a row whose switch is on, so the policy refuses a clear by itself,
--                       as the trigger of 0017 does (two locks). The column grant of 0006 (UPDATE (kill_switch) only) is unchanged: it cannot rename a tenant.
--
-- Not given a policy, on purpose: noema_migrator. No SECURITY DEFINER function reads or writes tenant, and foreign keys to tenant are checked as the table's owner with row security
-- off for that check (PostgreSQL runs the referential check as the owner without FORCE; TestForcedRowSecurityOnTenantDoesNotBreakForeignKeyChecksOfANonSuperuserOwner proves it
-- with a non-superuser owner). FORCE therefore makes the owner, and so the migration login, read and write no tenant row without a tenant mapping, as 0018 does for its tables.
-- A superuser still bypasses row security: the tenant list is created and administered by the operator's own superuser credential, the break-glass path of 0017, never by a service.
-- record_read holds no privilege on tenant, so its logins still cannot read the table. It is in the policy's role list, with EXECUTE on decision_login_tenant() (the function returns
-- only the caller's own tenant, as for the roles of 0012 and 0018), for one reason: the existing test TestNoRuntimeLoginCanClearTheKillSwitchEvenWithTheGrant simulates a mistaken
-- UPDATE grant to every per-tenant group, record_read included, and requires the trigger of 0017 to be what refuses the clear. A group that no policy covers would see zero rows
-- and never reach the trigger, which would change what that test shows. This grant is the one item of this migration that is not strictly needed to close DEBT-43; it is
-- listed for the Architect (status/SEC-08-tenant-rls.md), who may rule that the test case should change instead.

GRANT EXECUTE ON FUNCTION public.decision_login_tenant() TO record_read;

ALTER TABLE public.tenant ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.tenant FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_own_row ON public.tenant FOR ALL TO trust_rw, execution_rw, outcome_rw, evidence_rw, orchestrator_rw, record_read
  USING (tenant_id = public.decision_login_tenant()) WITH CHECK (tenant_id = public.decision_login_tenant());
CREATE POLICY tenant_verify_read ON public.tenant FOR SELECT TO record_verify USING (true);
CREATE POLICY tenant_verify_halt ON public.tenant FOR UPDATE TO record_verify USING (true) WITH CHECK (kill_switch);
