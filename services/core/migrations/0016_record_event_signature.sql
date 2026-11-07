-- 0016: database-side verification of Record events (REC-08; REQ-AUD-006; DEBT-03 and DEBT-08; DEC-2026-11-01 D2, D7, D11, D12, D13). Forward-only: never edited after merge.
-- It builds on 0012 (noema_sig.verify, signing_key, key_register); the number is provisional (A34, the noema#183 ledger).
--
-- Before it, record_insert_t checked only that an event extended the head, so a holder of one runtime credential and no key appended any well-formed
-- event under the emitter that credential is bound to (noema#236). After it:
--   * record_insert_t takes the signature, its key ID and the exact signed bytes as arguments; the unsigned form is dropped, so no caller can omit them;
--   * a BEFORE INSERT trigger on record_event, ENABLE ALWAYS and owned by noema_migrator, checks that the signed bytes ARE the stored body, that the body names
--     the row's own tenant, seq, prev_hash, event_id and written_by (so a signed event cannot be moved to another position, chain or emitter), and verifies
--     the Ed25519 signature over the domain tag `noema/record-event/v1\n` followed by the signed bytes, under an ACTIVE key of the written_by identity;
--   * it raises SQLSTATE NS001 otherwise; if verification is unavailable the insert fails closed (D13).
-- An emitter with no registered key cannot append: the default of OD-P-73 for the model service and the mocks. The signature is stored in columns, not as a
-- `sig` member of the event document: the v4 event contract of D1 is a separate change (A27), so the document and its hash are unchanged.

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'noema_migrator') THEN
    RAISE EXCEPTION 'the role noema_migrator does not exist: create it in deploy/compose/postgres/init.sh before migrating';
  END IF;
  IF EXISTS (SELECT 1 FROM public.record_event) THEN
    RAISE EXCEPTION 'record_event is not empty: an existing event has no signed bytes and no Ed25519 signature';
  END IF;
END $$;

ALTER TABLE public.record_event ADD COLUMN sig_key_id text NOT NULL;
ALTER TABLE public.record_event ADD COLUMN sig_value text NOT NULL;
ALTER TABLE public.record_event ADD COLUMN signed_jcs bytea NOT NULL;

CREATE FUNCTION public.record_event_verify() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
DECLARE doc jsonb; msg bytea;
BEGIN
  IF NEW.signed_jcs IS NULL OR NEW.sig_key_id IS NULL OR NEW.sig_value IS NULL THEN
    RAISE EXCEPTION 'signature not verified: the signed bytes or the signature are missing' USING ERRCODE = 'NS001';
  END IF;
  BEGIN
    doc := pg_catalog.convert_from(NEW.signed_jcs, 'UTF8')::jsonb;
  EXCEPTION WHEN others THEN
    RAISE EXCEPTION 'signature not verified: the signed bytes are not a JSON document' USING ERRCODE = 'NS001';
  END;
  IF jsonb_typeof(doc) <> 'object' OR doc ? 'sig' OR doc ? 'hash' THEN
    RAISE EXCEPTION 'signature not verified: the signed document must be an object without sig and without hash' USING ERRCODE = 'NS001';
  END IF;
  -- The stored body is the signed document, and the signed document names this row's own position and emitter (D7).
  IF NEW.body IS DISTINCT FROM doc
     OR jsonb_typeof(doc -> 'seq') IS DISTINCT FROM 'number'
     OR (doc ->> 'tenant_id') IS DISTINCT FROM NEW.tenant_id
     OR (doc ->> 'event_id') IS DISTINCT FROM NEW.event_id
     OR (doc ->> 'prev_hash') IS DISTINCT FROM NEW.prev_hash
     OR (doc ->> 'written_by') IS DISTINCT FROM NEW.written_by
     OR (doc ->> 'seq') IS DISTINCT FROM NEW.seq::text THEN
    RAISE EXCEPTION 'signature not verified: the signed document is not this event' USING ERRCODE = 'NS001';
  END IF;
  msg := pg_catalog.convert_to('noema/record-event/v1' || pg_catalog.chr(10), 'UTF8') || NEW.signed_jcs;
  IF NOT noema_sig.verify('record-event', NEW.written_by, NEW.sig_key_id, msg, NEW.sig_value) THEN
    RAISE EXCEPTION 'signature not verified' USING ERRCODE = 'NS001';
  END IF;
  RETURN NEW;
END $$;
ALTER FUNCTION public.record_event_verify() OWNER TO noema_migrator;
REVOKE ALL ON FUNCTION public.record_event_verify() FROM PUBLIC;
CREATE TRIGGER record_event_verify BEFORE INSERT ON public.record_event FOR EACH ROW EXECUTE FUNCTION public.record_event_verify();
ALTER TABLE public.record_event ENABLE ALWAYS TRIGGER record_event_verify;

-- The signed form of record_insert_t replaces the unsigned one. Same owner (record_append) and callers as before; search_path pinned to pg_catalog, pg_temp and
-- every relation named by its schema (ruling A6).
DROP FUNCTION public.record_insert_t(text, bigint, text, text, text, text, text, text, text, timestamptz, text, text, jsonb, jsonb);

GRANT CREATE ON SCHEMA public TO record_append;
CREATE FUNCTION public.record_insert_t(p_event_id text, p_seq bigint, p_prev_hash text, p_hash text, p_written_by text, p_event_type text, p_object_ref text,
  p_payload_hash text, p_correlation_id text, p_at timestamptz, p_schema_version text, p_actor text, p_body jsonb, p_payload jsonb,
  p_sig_key_id text, p_sig_value text, p_signed_jcs bytea) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
DECLARE v_tenant text := public.record_login_tenant('write'); head public.record_chain_head%ROWTYPE; v_emitter text;
BEGIN
  -- The emitter must be the one the login is bound to (security review of noema#145; DEBT-07): a login of one power cannot write the Record in another
  -- power's name, and an emitter that is not a power is written only by a login that the mapping binds to it.
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
  INSERT INTO public.record_event (event_id, tenant_id, seq, prev_hash, hash, written_by, event_type, object_ref, payload_hash, correlation_id, at, schema_version,
                                   actor, body, payload, sig_key_id, sig_value, signed_jcs)
  VALUES (p_event_id, v_tenant, p_seq, p_prev_hash, p_hash, p_written_by, p_event_type, p_object_ref, p_payload_hash, p_correlation_id, p_at, p_schema_version,
          p_actor, p_body, p_payload, p_sig_key_id, p_sig_value, p_signed_jcs);
  UPDATE public.record_chain_head SET seq = p_seq, hash = p_hash WHERE tenant_id = v_tenant;
END $$;
ALTER FUNCTION public.record_insert_t(text, bigint, text, text, text, text, text, text, text, timestamptz, text, text, jsonb, jsonb, text, text, bytea) OWNER TO record_append;
REVOKE CREATE ON SCHEMA public FROM record_append;
REVOKE ALL ON FUNCTION public.record_insert_t(text, bigint, text, text, text, text, text, text, text, timestamptz, text, text, jsonb, jsonb, text, text, bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.record_insert_t(text, bigint, text, text, text, text, text, text, text, timestamptz, text, text, jsonb, jsonb, text, text, bytea)
  TO trust_rw, execution_rw, outcome_rw, evidence_rw, orchestrator_rw, record_emitter;

-- REC-08 (D5, D7, row 13): the chain verifier checks every signature in Go, with the public keys, so it reads the key registry. It holds no other new privilege.
GRANT SELECT ON public.signing_key TO record_verify;
