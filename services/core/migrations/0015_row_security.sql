-- 0015: row-level security with FORCE on the Execution, Outcome and Record tables (ruling A6 of 6 Nov 2026; REQ-SEC-004, INV-12, REQ-AUD-001, REQ-AUD-003).
-- Forward-only: never edited after merge. Row security restricts which rows a login sees and writes; it does not authenticate a receipt or a Record event.
--
-- The tenant of a row is the tenant of the login that connected: decision_login_tenant() (migration 0009, noema#168) reads tenant_login by session_user,
-- which no caller can change. Each table below gets one policy for the roles that can touch it. FORCE makes the owner subject to the policies too, so
-- a migration or administration role that is not a superuser reads and writes no tenant's rows without a tenant mapping. A superuser still bypasses row
-- security in PostgreSQL, whatever is set; no runtime login is one.
--
-- noema_migrator is named on the tables its SECURITY DEFINER functions write (receipt_insert and its trigger, outcome_record_verdict): a definer function
-- runs as its owner, and the owner is subject to FORCE. current_user is the owner there, but the tenant still comes from session_user (the login), so
-- naming the owner opens nothing to a role that has no mapped login. record_verify, the chain verifier (REC-05), is the one role that reads every
-- tenant, by design and by a role-scoped SELECT policy; it holds no write privilege.

GRANT EXECUTE ON FUNCTION decision_login_tenant() TO orchestrator_rw, evidence_rw, record_append, noema_migrator;

DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['grant_spend', 'idempotency_record', 'execution_receipt', 'recovery_request', 'outbox_event', 'outcome_verdict', 'outcome_watch'] LOOP
    EXECUTE format('ALTER TABLE public.%I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE public.%I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY %I ON public.%I FOR ALL TO trust_rw, execution_rw, outcome_rw, evidence_rw, orchestrator_rw, noema_migrator '
                   'USING (tenant_id = public.decision_login_tenant()) WITH CHECK (tenant_id = public.decision_login_tenant())', t || '_tenant', t);
  END LOOP;
END $$;

-- The Record: its writers are the SECURITY DEFINER functions of record_append, which find the tenant by the same mapping; the verifier reads all tenants.
ALTER TABLE public.record_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.record_event FORCE ROW LEVEL SECURITY;
CREATE POLICY record_event_tenant ON public.record_event FOR ALL TO record_append
  USING (tenant_id = public.decision_login_tenant()) WITH CHECK (tenant_id = public.decision_login_tenant());
CREATE POLICY record_event_verify ON public.record_event FOR SELECT TO record_verify USING (true);

ALTER TABLE public.record_chain_head ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.record_chain_head FORCE ROW LEVEL SECURITY;
CREATE POLICY record_chain_head_tenant ON public.record_chain_head FOR ALL TO record_append
  USING (tenant_id = public.decision_login_tenant()) WITH CHECK (tenant_id = public.decision_login_tenant());
CREATE POLICY record_chain_head_verify ON public.record_chain_head FOR SELECT TO record_verify USING (true);
