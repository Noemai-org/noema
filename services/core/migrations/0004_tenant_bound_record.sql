-- 0004: the tenant of a Record call comes from the login, never from the caller (DEBT-04, Tier 0; option A).
-- REQ-SEC-004 (tenant isolation enforced in the database), INV-12, REQ-AUD-001, REQ-AUD-003.
-- Forward-only: this file is never edited after merge (AGENTS.md section 7).
--
-- Slice 1 of DEBT-04 option A: it ADDS the mapping and the tenant-bound functions. The old functions
-- (record_lock_head(text), record_insert(...), record_read_chain(text)) stay callable until the Go callers have moved to
-- the new ones; a later migration revokes them. Until then the old functions are the known gap that DEBT-04 records.
--
-- Design: each connection logs in as a role that is mapped to exactly one tenant. The new functions find the tenant
-- with session_user (the login the connection authenticated as). session_user is not changed by SECURITY DEFINER or by
-- SET ROLE, and only a superuser can change it, so a caller cannot choose the tenant.

CREATE TABLE tenant_login (
  login_role text PRIMARY KEY,
  power      text NOT NULL CHECK (power IN ('trust', 'execution', 'outcome', 'evidence', 'orchestrator', 'record_read')),
  tenant_id  text NOT NULL REFERENCES tenant (tenant_id),
  -- NULL: the login writes as its power's own emitter. Set: the login writes as this one emitter and nothing else. This is how an
  -- emitter that is not a power (edge, record, admin, the model service, the mocks) gets an identity the database can check
  -- (DEBT-07, docs#106 R16). No production login sets it until EDG-01 provisions the writers.
  emitter    text CHECK (emitter IS NULL OR emitter ~ '^[a-z][a-z0-9-]*$')
);
ALTER TABLE tenant_login OWNER TO record_append;
REVOKE ALL ON tenant_login FROM PUBLIC;

-- The tenant of the calling login, for a write ('write') or a read ('read'). A login with no mapping, or whose power does
-- not fit the kind of call, is refused. Owned by record_append and callable only by the functions below.
CREATE FUNCTION record_login_tenant(p_kind text) RETURNS text
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE t text; pw text;
BEGIN
  SELECT l.tenant_id, l.power INTO t, pw FROM tenant_login l WHERE l.login_role = session_user::text;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'record: this login is not mapped to a tenant' USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF (p_kind = 'write' AND pw = 'record_read') OR (p_kind = 'read' AND pw <> 'record_read') OR p_kind NOT IN ('write', 'read') THEN
    RAISE EXCEPTION 'record: this login may not make a % call', p_kind USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN t;
END $$;
ALTER FUNCTION record_login_tenant(text) OWNER TO record_append;
REVOKE ALL ON FUNCTION record_login_tenant(text) FROM PUBLIC;

CREATE FUNCTION record_lock_head_t() RETURNS TABLE (seq bigint, hash text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE v_tenant text := record_login_tenant('write');
BEGIN
  INSERT INTO record_chain_head (tenant_id, seq, hash)
  VALUES (v_tenant, 0, 'sha256:' || encode(sha256(convert_to('noema:genesis:' || v_tenant, 'UTF8')), 'hex'))
  ON CONFLICT (tenant_id) DO NOTHING;
  RETURN QUERY SELECT h.seq, h.hash FROM record_chain_head h WHERE h.tenant_id = v_tenant FOR UPDATE;
END $$;

CREATE FUNCTION record_insert_t(
  p_event_id text, p_seq bigint, p_prev_hash text, p_hash text, p_written_by text, p_event_type text,
  p_object_ref text, p_payload_hash text, p_correlation_id text, p_at timestamptz, p_schema_version text, p_actor text, p_body jsonb, p_payload jsonb
) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE v_tenant text := record_login_tenant('write'); head record_chain_head%ROWTYPE; v_emitter text;
BEGIN
  -- The emitter must be the one the login is bound to (security review of noema#145, finding 1; DEBT-07): a login of one power
  -- cannot write the Record in another power's name, and an emitter that is not a power is written only by a login that the
  -- mapping binds to it. Anything else is refused, including every emitter that has no bound login yet.
  SELECT COALESCE(l.emitter, CASE l.power WHEN 'trust' THEN 'core-trust' WHEN 'execution' THEN 'core-execution' WHEN 'outcome' THEN 'core-outcome'
                      WHEN 'evidence' THEN 'core-evidence' WHEN 'orchestrator' THEN 'core-workflows' END)
    INTO v_emitter FROM tenant_login l WHERE l.login_role = session_user::text;
  IF p_written_by IS DISTINCT FROM v_emitter THEN
    RAISE EXCEPTION 'record chain: written_by is not the emitter bound to the login' USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF p_body ->> 'tenant_id' IS DISTINCT FROM v_tenant THEN
    RAISE EXCEPTION 'record chain: the event names another tenant than the login''s' USING ERRCODE = 'insufficient_privilege';
  END IF;
  SELECT * INTO head FROM record_chain_head h WHERE h.tenant_id = v_tenant FOR UPDATE;
  IF NOT FOUND OR head.seq + 1 <> p_seq OR head.hash <> p_prev_hash THEN
    RAISE EXCEPTION 'record chain: the event does not extend the head of the login''s tenant' USING ERRCODE = 'serialization_failure';
  END IF;
  INSERT INTO record_event (event_id, tenant_id, seq, prev_hash, hash, written_by, event_type, object_ref, payload_hash, correlation_id, at, schema_version, actor, body, payload)
  VALUES (p_event_id, v_tenant, p_seq, p_prev_hash, p_hash, p_written_by, p_event_type, p_object_ref, p_payload_hash, p_correlation_id, p_at, p_schema_version, p_actor, p_body, p_payload);
  UPDATE record_chain_head SET seq = p_seq, hash = p_hash WHERE tenant_id = v_tenant;
END $$;

CREATE FUNCTION record_read_chain_t() RETURNS TABLE (
  seq bigint, event_id text, prev_hash text, hash text, written_by text, actor text, event_type text, object_ref text,
  payload_hash text, correlation_id text, schema_version text, body text, payload text
)
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE v_tenant text := record_login_tenant('read');
BEGIN
  RETURN QUERY SELECT e.seq, e.event_id, e.prev_hash, e.hash, e.written_by, e.actor, e.event_type, e.object_ref, e.payload_hash,
         e.correlation_id, e.schema_version, e.body::text, e.payload::text
  FROM record_event e WHERE e.tenant_id = v_tenant ORDER BY e.seq;
END $$;

ALTER FUNCTION record_lock_head_t() OWNER TO record_append;
ALTER FUNCTION record_insert_t(text, bigint, text, text, text, text, text, text, text, timestamptz, text, text, jsonb, jsonb) OWNER TO record_append;
ALTER FUNCTION record_read_chain_t() OWNER TO record_append;

REVOKE ALL ON FUNCTION record_lock_head_t(), record_insert_t(text, bigint, text, text, text, text, text, text, text, timestamptz, text, text, jsonb, jsonb), record_read_chain_t() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION record_lock_head_t(), record_insert_t(text, bigint, text, text, text, text, text, text, text, timestamptz, text, text, jsonb, jsonb)
  TO trust_rw, execution_rw, outcome_rw, evidence_rw, orchestrator_rw;
GRANT EXECUTE ON FUNCTION record_read_chain_t() TO record_read;
