-- 0014: harden the Record functions (ruling A6 of 6 Nov 2026; REQ-SEC-004, REQ-AUD-001, INV-12, INV-13). Forward-only: never edited after merge.
--
-- Every SECURITY DEFINER function owned by record_append now pins search_path to `pg_catalog, pg_temp` and names every table and function by its
-- schema. With `pg_catalog, public` the session's temporary schema is searched first for a relation, so a login holding TEMPORARY could create
-- pg_temp.tenant_login (granting SELECT on it to record_append, which it owns the right to do on its own table) and make record_login_tenant
-- return a tenant of its choice: it then wrote and read another tenant's chain (TestATemporaryTenantLoginTableDoesNotRedirectARecordCall).
-- Bodies are unchanged except for the qualification. CREATE OR REPLACE keeps each function's owner and privileges.

CREATE OR REPLACE FUNCTION public.provision_tenant_login(p_login text, p_power text, p_tenant text)
 RETURNS void
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
DECLARE v_group text;
BEGIN
  v_group := CASE p_power WHEN 'record_read' THEN 'record_read' ELSE p_power || '_rw' END;
  IF NOT EXISTS (SELECT 1 FROM pg_roles r WHERE r.rolname = p_login AND r.rolcanlogin) THEN
    RAISE EXCEPTION 'provision: % is not a login role: create it in deploy/compose/postgres/init.sh first', p_login;
  END IF;
  IF NOT pg_has_role(p_login, v_group, 'MEMBER') THEN
    RAISE EXCEPTION 'provision: % is not a member of %', p_login, v_group;
  END IF;
  INSERT INTO public.tenant_login (login_role, power, tenant_id) VALUES (p_login, p_power, p_tenant);
END $function$;

CREATE OR REPLACE FUNCTION public.record_insert_t(p_event_id text, p_seq bigint, p_prev_hash text, p_hash text, p_written_by text, p_event_type text, p_object_ref text, p_payload_hash text, p_correlation_id text, p_at timestamp with time zone, p_schema_version text, p_actor text, p_body jsonb, p_payload jsonb)
 RETURNS void
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
DECLARE v_tenant text := public.record_login_tenant('write'); head public.record_chain_head%ROWTYPE; v_emitter text;
BEGIN
  -- The emitter must be the one the login is bound to (security review of noema#145, finding 1; DEBT-07): a login of one power
  -- cannot write the Record in another power's name, and an emitter that is not a power is written only by a login that the
  -- mapping binds to it. Anything else is refused, including every emitter that has no bound login yet.
  SELECT COALESCE(l.emitter, CASE l.power WHEN 'trust' THEN 'core-trust' WHEN 'execution' THEN 'core-execution' WHEN 'outcome' THEN 'core-outcome'
                      WHEN 'evidence' THEN 'core-evidence' WHEN 'orchestrator' THEN 'core-workflows' END)
    INTO v_emitter FROM public.tenant_login l WHERE l.login_role = session_user::text;
  IF p_written_by IS DISTINCT FROM v_emitter THEN
    RAISE EXCEPTION 'record chain: written_by is not the emitter bound to the login' USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF p_body ->> 'tenant_id' IS DISTINCT FROM v_tenant THEN
    RAISE EXCEPTION 'record chain: the event names another tenant than the login''s' USING ERRCODE = 'insufficient_privilege';
  END IF;
  SELECT * INTO head FROM public.record_chain_head h WHERE h.tenant_id = v_tenant FOR UPDATE;
  IF NOT FOUND OR head.seq + 1 <> p_seq OR head.hash <> p_prev_hash THEN
    RAISE EXCEPTION 'record chain: the event does not extend the head of the login''s tenant' USING ERRCODE = 'serialization_failure';
  END IF;
  INSERT INTO public.record_event (event_id, tenant_id, seq, prev_hash, hash, written_by, event_type, object_ref, payload_hash, correlation_id, at, schema_version, actor, body, payload)
  VALUES (p_event_id, v_tenant, p_seq, p_prev_hash, p_hash, p_written_by, p_event_type, p_object_ref, p_payload_hash, p_correlation_id, p_at, p_schema_version, p_actor, p_body, p_payload);
  UPDATE public.record_chain_head SET seq = p_seq, hash = p_hash WHERE tenant_id = v_tenant;
END $function$;

CREATE OR REPLACE FUNCTION public.record_lock_head_t()
 RETURNS TABLE(seq bigint, hash text)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
DECLARE v_tenant text := public.record_login_tenant('write');
BEGIN
  INSERT INTO public.record_chain_head (tenant_id, seq, hash)
  VALUES (v_tenant, 0, 'sha256:' || encode(sha256(convert_to('noema:genesis:' || v_tenant, 'UTF8')), 'hex'))
  ON CONFLICT (tenant_id) DO NOTHING;
  RETURN QUERY SELECT h.seq, h.hash FROM public.record_chain_head h WHERE h.tenant_id = v_tenant FOR UPDATE;
END $function$;

CREATE OR REPLACE FUNCTION public.record_login_tenant(p_kind text)
 RETURNS text
 LANGUAGE plpgsql
 STABLE SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
DECLARE t text; pw text;
BEGIN
  SELECT l.tenant_id, l.power INTO t, pw FROM public.tenant_login l WHERE l.login_role = session_user::text;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'record: this login is not mapped to a tenant' USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF (p_kind = 'write' AND pw = 'record_read') OR (p_kind = 'read' AND pw <> 'record_read') OR p_kind NOT IN ('write', 'read') THEN
    RAISE EXCEPTION 'record: this login may not make a % call', p_kind USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN t;
END $function$;

CREATE OR REPLACE FUNCTION public.record_read_chain_t()
 RETURNS TABLE(seq bigint, event_id text, prev_hash text, hash text, written_by text, actor text, event_type text, object_ref text, payload_hash text, correlation_id text, schema_version text, body text, payload text)
 LANGUAGE plpgsql
 STABLE SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
DECLARE v_tenant text := public.record_login_tenant('read');
BEGIN
  RETURN QUERY SELECT e.seq, e.event_id, e.prev_hash, e.hash, e.written_by, e.actor, e.event_type, e.object_ref, e.payload_hash,
         e.correlation_id, e.schema_version, e.body::text, e.payload::text
  FROM public.record_event e WHERE e.tenant_id = v_tenant ORDER BY e.seq;
END $function$;