-- 0016: harden the Record functions (ruling A6 of 6 Nov 2026; REQ-SEC-004, REQ-AUD-001, INV-12, INV-13). Forward-only: never edited after merge.
--
-- Every SECURITY DEFINER function owned by record_append (and, at the end, the three of migration 0007) now pins search_path to `pg_catalog, pg_temp` and names every table and function by its
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

-- SR design review of noema#234 (7 Nov 2026): the three SECURITY DEFINER functions of migration 0007 (Outcome's verdict path) get the same pin and
-- the same qualification. Bodies are unchanged except for `public.` before the five tables they read or write. CREATE OR REPLACE keeps owner and ACL.

CREATE OR REPLACE FUNCTION public.outcome_record_verdict(p_tenant text, p_verdict_id text, p_receipt_id text, p_verification_id text, p_correlation_id text, p_outcome_definition text, p_verdict text, p_reason text, p_predicate_result boolean, p_evidence_reads text[], p_verified_at timestamp with time zone, p_sig_key_id text, p_sig_value text, p_json jsonb, p_recovery_request_id text, p_watch_id text, p_next_check_at timestamp with time zone, p_deadline_at timestamp with time zone, p_triggers text[])
 RETURNS void
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
DECLARE v_attempt bigint; v_corr text;
BEGIN
  -- Authenticate the calling login. EXECUTE already limits the call to members of outcome_rw, but a login can run as that role
  -- (SET ROLE) while also being something else. session_user is the login that connected, whatever role it runs as. Outcome's
  -- login holds the outcome_rw power and no other per-power role (BD-05, INV-04). A superuser counts as a member of every
  -- role, so it is refused here too. This is not proof of who holds the credential; that is REC-08.
  IF pg_has_role(session_user, 'trust_rw', 'MEMBER') OR pg_has_role(session_user, 'execution_rw', 'MEMBER')
     OR pg_has_role(session_user, 'evidence_rw', 'MEMBER') OR pg_has_role(session_user, 'orchestrator_rw', 'MEMBER') THEN
    RAISE EXCEPTION 'outcome_record_verdict: the calling login is not Outcome''s own' USING ERRCODE = 'insufficient_privilege';
  END IF;
  -- The tenant comes from the login, not from the caller's argument (SR's P1 on noema#234; REQ-SEC-004, INV-12, DEBT-08): Outcome's login for one tenant
  -- cannot record a verdict, a Record event or an outbox row for another tenant by passing that tenant as p_tenant.
  IF p_tenant IS DISTINCT FROM public.record_login_tenant('write') THEN
    RAISE EXCEPTION 'outcome_record_verdict: the tenant is not the one the calling login is mapped to' USING ERRCODE = 'insufficient_privilege';
  END IF;
  SELECT r.attempt, g.json->>'correlation_id' INTO v_attempt, v_corr
    FROM public.execution_receipt r JOIN public.decision_grant g ON g.grant_id = r.grant_id AND g.tenant_id = r.tenant_id
    WHERE r.receipt_id = p_receipt_id AND r.tenant_id = p_tenant;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'outcome_record_verdict: receipt % does not exist for tenant %', p_receipt_id, p_tenant USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  -- The verdict belongs to the receipt's own grant and says in its columns what its signed document says. This bounds a
  -- caller that is not Outcome; it does not prove who called (REC-08).
  IF v_corr IS DISTINCT FROM p_correlation_id THEN
    RAISE EXCEPTION 'outcome_record_verdict: the correlation id is not the one of the receipt''s grant' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  IF (p_json->>'verdict_id', p_json->>'tenant_id', p_json->>'receipt_id', p_json->>'verdict', p_json->>'correlation_id', p_json->>'outcome_definition')
     IS DISTINCT FROM (p_verdict_id, p_tenant, p_receipt_id, p_verdict, p_correlation_id, p_outcome_definition) THEN
    RAISE EXCEPTION 'outcome_record_verdict: the document and the columns disagree' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  -- Every other field the document and the columns both hold: the evidence the verdict relied on, its predicate result, the
  -- instant it was made and its signature.
  IF p_json->'evidence_reads' IS DISTINCT FROM to_jsonb(p_evidence_reads)
     OR p_json->'predicate_result' IS DISTINCT FROM COALESCE(to_jsonb(p_predicate_result), 'null'::jsonb)
     OR (p_json->>'verified_at')::timestamptz IS DISTINCT FROM p_verified_at
     OR (p_json#>>'{sig,key_id}', p_json#>>'{sig,value}') IS DISTINCT FROM (p_sig_key_id, p_sig_value) THEN
    RAISE EXCEPTION 'outcome_record_verdict: the signed document and the columns disagree on the evidence, the predicate, the instant or the signature' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  -- The durable watch is part of the signed document: the row that OUT-03 schedules from says what the verdict says.
  IF p_verdict = 'PENDING_EXTERNAL' THEN
    IF p_watch_id IS NULL OR p_next_check_at IS NULL OR p_deadline_at IS NULL OR p_triggers IS NULL THEN
      RAISE EXCEPTION 'outcome_record_verdict: a PENDING_EXTERNAL verdict needs a watch with a deadline' USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    IF jsonb_typeof(p_json->'watch') IS DISTINCT FROM 'object'
       OR (p_json#>>'{watch,watch_id}') IS DISTINCT FROM p_watch_id
       OR (p_json#>>'{watch,next_check_at}')::timestamptz IS DISTINCT FROM p_next_check_at
       OR (p_json#>>'{watch,deadline_at}')::timestamptz IS DISTINCT FROM p_deadline_at
       OR p_json#>'{watch,triggers}' IS DISTINCT FROM to_jsonb(p_triggers) THEN
      RAISE EXCEPTION 'outcome_record_verdict: the signed document and the watch disagree' USING ERRCODE = 'integrity_constraint_violation';
    END IF;
  ELSIF p_json ? 'watch' THEN
    RAISE EXCEPTION 'outcome_record_verdict: only a PENDING_EXTERNAL verdict has a watch' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  INSERT INTO public.outcome_verdict (verdict_id, tenant_id, correlation_id, receipt_id, verification_id, outcome_definition, verdict, reason, predicate_result,
                               evidence_reads, verified_at, json, sig_key_id, sig_value)
  VALUES (p_verdict_id, p_tenant, p_correlation_id, p_receipt_id, p_verification_id, p_outcome_definition, p_verdict, p_reason, p_predicate_result,
          p_evidence_reads, p_verified_at, p_json, p_sig_key_id, p_sig_value);
  IF p_verdict <> 'ACHIEVED' THEN
    IF p_recovery_request_id IS NULL THEN
      RAISE EXCEPTION 'outcome_record_verdict: a % verdict needs its recovery request id', p_verdict USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    INSERT INTO public.recovery_request (recovery_request_id, tenant_id, grant_id, verdict_id, reason, attempt_count, created_at)
    VALUES (p_recovery_request_id, p_tenant, NULL, p_verdict_id, p_reason, v_attempt, p_verified_at);
  ELSIF p_recovery_request_id IS NOT NULL THEN
    RAISE EXCEPTION 'outcome_record_verdict: an ACHIEVED verdict has no recovery request' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  IF p_verdict = 'PENDING_EXTERNAL' THEN
    IF p_watch_id IS NULL OR p_next_check_at IS NULL OR p_deadline_at IS NULL OR p_triggers IS NULL THEN
      RAISE EXCEPTION 'outcome_record_verdict: a PENDING_EXTERNAL verdict needs a watch with a deadline' USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    INSERT INTO public.outcome_watch (watch_id, tenant_id, verdict_id, next_check_at, deadline_at, triggers, created_at)
    VALUES (p_watch_id, p_tenant, p_verdict_id, p_next_check_at, p_deadline_at, p_triggers, p_verified_at);
  ELSIF p_watch_id IS NOT NULL THEN
    RAISE EXCEPTION 'outcome_record_verdict: only a PENDING_EXTERNAL verdict has a watch' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
END $function$
;

CREATE OR REPLACE FUNCTION public.outcome_verdict_complete()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
DECLARE has_request boolean; has_watch boolean;
BEGIN
  SELECT EXISTS (SELECT 1 FROM public.recovery_request r WHERE r.verdict_id = NEW.verdict_id) INTO has_request;
  SELECT EXISTS (SELECT 1 FROM public.outcome_watch w WHERE w.verdict_id = NEW.verdict_id) INTO has_watch;
  IF (NEW.verdict <> 'ACHIEVED') <> has_request THEN
    RAISE EXCEPTION 'public.outcome_verdict %: a verdict has a recovery request exactly when it is not ACHIEVED', NEW.verdict_id USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  IF (NEW.verdict = 'PENDING_EXTERNAL') <> has_watch THEN
    RAISE EXCEPTION 'public.outcome_verdict %: a verdict has a watch exactly when it is PENDING_EXTERNAL', NEW.verdict_id USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  RETURN NULL;
END $function$
;

CREATE OR REPLACE FUNCTION public.recovery_request_verdict_guard()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
BEGIN
  IF NEW.verdict_id IS NOT NULL AND NOT EXISTS (
       SELECT 1 FROM public.outcome_verdict v WHERE v.verdict_id = NEW.verdict_id AND v.tenant_id = NEW.tenant_id AND v.verdict <> 'ACHIEVED') THEN
    RAISE EXCEPTION 'public.recovery_request: % does not answer a non-ACHIEVED verdict of tenant %', NEW.verdict_id, NEW.tenant_id USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  RETURN NEW;
END $function$
;

-- The function above is owned by the migration role and now asks record_login_tenant (owned by record_append, callable by no one else) for the login's tenant.
GRANT EXECUTE ON FUNCTION public.record_login_tenant(text) TO noema_migrator;

-- SR's note 1 on noema#234: the trigger functions of 0001 and 0007 that read no relation are pinned too (ALTER FUNCTION keeps the body, so nothing else changes).
-- decision_grant_guard reads two tables and is rewritten, qualified and pinned, in noema#233 (migration 0014).
ALTER FUNCTION public.reject_mutation() SET search_path = pg_catalog, pg_temp;
ALTER FUNCTION public.idempotency_record_guard() SET search_path = pg_catalog, pg_temp;
ALTER FUNCTION public.outcome_watch_guard() SET search_path = pg_catalog, pg_temp;
