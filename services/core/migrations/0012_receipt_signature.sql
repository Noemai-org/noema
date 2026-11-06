-- 0012: database-side verification of execution receipts (Ed25519 through pgsodium). EXE-08 (W4) slice B; DEBT-01, Option B.
-- DEC-2026-11-01-receipt-and-record-signing D4 to D8, D10 to D13, D15, D16; rulings A2 and A16 (docs#117). Forward-only: never edited after merge.
--
-- Before this migration, execution_rw could INSERT any row into execution_receipt, so a forged REJECTED_STALE_VERSION receipt (any 64 hex
-- characters as its "signature") was accepted, and the SPENT-to-VOID guard of noema#94 would have trusted it (DEBT-01, Tier 0). After it:
--   * a receipt reaches the table only through receipt_insert(), a SECURITY DEFINER function owned by noema_migrator;
--   * a BEFORE INSERT trigger, ENABLE ALWAYS, rebuilds the signed message from the stored signed bytes, checks that they say what the row says
--     and what grant_spend recorded, and verifies the Ed25519 signature against a public key in signing_key; it raises NS001 otherwise;
--   * the private key is never in PostgreSQL: only public keys are registered, by key_register(), which no runtime role can execute;
--   * if verification is unavailable (no extension, no library, the function raising) the insert fails and nothing is kept (D13).
-- Every object below is owned by noema_migrator and every function pins search_path and schema-qualifies what it touches (ruling A6).
-- Prerequisite, done once by the bootstrap superuser before any migration (D10a; deploy/compose/postgres/init.sh): CREATE EXTENSION pgsodium,
-- its event triggers disabled, EXECUTE on pgsodium.crypto_sign_verify_detached for noema_migrator and for nobody else.
--
-- The table is empty everywhere (no receipt exists outside tests and throwaway databases); the guard below makes that a failure, not an assumption.

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'noema_migrator') THEN
    RAISE EXCEPTION 'the role noema_migrator does not exist: create it in deploy/compose/postgres/init.sh before migrating';
  END IF;
  IF EXISTS (SELECT 1 FROM public.execution_receipt) THEN
    RAISE EXCEPTION 'execution_receipt is not empty: an existing receipt has no signed bytes and no Ed25519 signature';
  END IF;
END $$;

CREATE SCHEMA noema_sig AUTHORIZATION noema_migrator;
REVOKE ALL ON SCHEMA noema_sig FROM PUBLIC;

-- The public keys (D3, D4, D5). key_id is derived, never supplied (invariant K1).
CREATE TABLE public.signing_key (
  key_id            text PRIMARY KEY,
  identity          text NOT NULL,
  version           integer NOT NULL CHECK (version >= 1),
  algorithm         text NOT NULL DEFAULT 'ED25519' CHECK (algorithm = 'ED25519'),
  public_key        bytea NOT NULL CHECK (octet_length(public_key) = 32),
  status            text NOT NULL CHECK (status IN ('ACTIVE', 'RETIRED', 'REVOKED')),
  registered_at     timestamptz NOT NULL,
  retired_at        timestamptz,
  compromised_since timestamptz,
  CONSTRAINT signing_key_identity_version_key UNIQUE (identity, version),
  CONSTRAINT signing_key_id_binds_identity_and_version CHECK (key_id = identity || '.' || lower(algorithm) || '.' || version::text),
  CONSTRAINT signing_key_id_shape CHECK (key_id ~ '^[a-z][a-z-]*\.ed25519\.[1-9][0-9]*$'),
  CONSTRAINT signing_key_state CHECK (
    (status = 'ACTIVE'  AND retired_at IS NULL AND compromised_since IS NULL) OR
    (status = 'RETIRED' AND retired_at IS NOT NULL AND compromised_since IS NULL) OR
    (status = 'REVOKED' AND compromised_since IS NOT NULL))
);
CREATE INDEX signing_key_identity_status_idx ON public.signing_key (identity, status);
ALTER TABLE public.signing_key OWNER TO noema_migrator;

-- Which identities may hold a signing key (D2). model-service, the adapter emitters and benchmark-runner have none (OD-P-73's default).
CREATE FUNCTION noema_sig.signing_identity(p_identity text) RETURNS boolean
LANGUAGE sql IMMUTABLE SET search_path = pg_catalog, pg_temp AS $$
  SELECT p_identity IN ('core-execution', 'core-outcome', 'core-trust', 'core-evidence', 'core-workflows', 'core-edge', 'core-record', 'admin')
$$;

-- Immutability and the rules of D5 as a trigger, so a path around the three key functions still meets them.
CREATE FUNCTION noema_sig.signing_key_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
DECLARE active_n integer; newest integer;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'signing_key: DELETE is not allowed' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  IF current_setting('noema_sig.key_change', true) IS DISTINCT FROM 'on' THEN
    RAISE EXCEPTION 'signing_key: change it only through key_register, key_retire or key_revoke' USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NOT noema_sig.signing_identity(NEW.identity) THEN
      RAISE EXCEPTION 'signing_key: % is not an identity that holds a signing key', NEW.identity USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    IF NEW.status <> 'ACTIVE' THEN
      RAISE EXCEPTION 'signing_key: a key is registered ACTIVE' USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    -- One registration per identity at a time: two concurrent registrations would each count one ACTIVE key and both pass (Codex review of noema#229).
    -- The lock is held to the end of the transaction, and the counts below are taken after it, so they see the other registration once it commits.
    PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('noema_sig.signing_key:' || NEW.identity, 0));
    SELECT count(*), COALESCE(max(k.version), 0) INTO active_n, newest FROM public.signing_key k WHERE k.identity = NEW.identity AND k.status = 'ACTIVE';
    IF active_n >= 2 THEN
      RAISE EXCEPTION 'signing_key: % already has two ACTIVE keys (the rotation overlap)', NEW.identity USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    SELECT COALESCE(max(k.version), 0) INTO newest FROM public.signing_key k WHERE k.identity = NEW.identity;
    IF NEW.version <= newest THEN
      RAISE EXCEPTION 'signing_key: version % of % is not newer than version %', NEW.version, NEW.identity, newest USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    RETURN NEW;
  END IF;
  -- UPDATE: nothing but the status moves, forward only: ACTIVE to RETIRED or REVOKED, RETIRED to REVOKED.
  IF (NEW.key_id, NEW.identity, NEW.version, NEW.algorithm, NEW.public_key, NEW.registered_at)
     IS DISTINCT FROM (OLD.key_id, OLD.identity, OLD.version, OLD.algorithm, OLD.public_key, OLD.registered_at) THEN
    RAISE EXCEPTION 'signing_key: only the status of a key may change' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  IF NOT ((OLD.status = 'ACTIVE' AND NEW.status IN ('RETIRED', 'REVOKED')) OR (OLD.status = 'RETIRED' AND NEW.status = 'REVOKED')) THEN
    RAISE EXCEPTION 'signing_key: % to % is not a change of status', OLD.status, NEW.status USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER signing_key_guard BEFORE INSERT OR UPDATE OR DELETE ON public.signing_key FOR EACH ROW EXECUTE FUNCTION noema_sig.signing_key_guard();
ALTER TABLE public.signing_key ENABLE ALWAYS TRIGGER signing_key_guard;
CREATE TRIGGER signing_key_no_truncate BEFORE TRUNCATE ON public.signing_key FOR EACH STATEMENT EXECUTE FUNCTION public.reject_mutation();
ALTER TABLE public.signing_key ENABLE ALWAYS TRIGGER signing_key_no_truncate;

-- The only ways to change the key table. The deploy step calls key_register (D3a); no migration holds key bytes.
CREATE FUNCTION public.key_register(p_identity text, p_version integer, p_public_key bytea) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
DECLARE v_id text;
BEGIN
  v_id := p_identity || '.ed25519.' || p_version::text;
  PERFORM pg_catalog.set_config('noema_sig.key_change', 'on', true);
  INSERT INTO public.signing_key (key_id, identity, version, algorithm, public_key, status, registered_at)
  VALUES (v_id, p_identity, p_version, 'ED25519', p_public_key, 'ACTIVE', pg_catalog.clock_timestamp());
  PERFORM pg_catalog.set_config('noema_sig.key_change', 'off', true);
  RETURN v_id;
END $$;

CREATE FUNCTION public.key_retire(p_key_id text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  PERFORM pg_catalog.set_config('noema_sig.key_change', 'on', true);
  UPDATE public.signing_key SET status = 'RETIRED', retired_at = pg_catalog.clock_timestamp() WHERE key_id = p_key_id AND status = 'ACTIVE';
  IF NOT FOUND THEN
    RAISE EXCEPTION 'key_retire: % is not an ACTIVE key', p_key_id USING ERRCODE = 'no_data_found';
  END IF;
  PERFORM pg_catalog.set_config('noema_sig.key_change', 'off', true);
END $$;

CREATE FUNCTION public.key_revoke(p_key_id text, p_compromised_since timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  PERFORM pg_catalog.set_config('noema_sig.key_change', 'on', true);
  UPDATE public.signing_key SET status = 'REVOKED', compromised_since = p_compromised_since, retired_at = NULL
   WHERE key_id = p_key_id AND status IN ('ACTIVE', 'RETIRED');
  IF NOT FOUND THEN
    RAISE EXCEPTION 'key_revoke: % is not an ACTIVE or RETIRED key', p_key_id USING ERRCODE = 'no_data_found';
  END IF;
  PERFORM pg_catalog.set_config('noema_sig.key_change', 'off', true);
END $$;

ALTER FUNCTION noema_sig.signing_identity(text) OWNER TO noema_migrator;
ALTER FUNCTION noema_sig.signing_key_guard() OWNER TO noema_migrator;
ALTER FUNCTION public.key_register(text, integer, bytea) OWNER TO noema_migrator;
ALTER FUNCTION public.key_retire(text) OWNER TO noema_migrator;
ALTER FUNCTION public.key_revoke(text, timestamptz) OWNER TO noema_migrator;
REVOKE ALL ON FUNCTION public.key_register(text, integer, bytea), public.key_retire(text), public.key_revoke(text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION noema_sig.signing_identity(text), noema_sig.signing_key_guard() FROM PUBLIC;

-- D11: the one verification function. No exception handler: an error from pgsodium or a missing extension aborts the statement.
CREATE FUNCTION noema_sig.verify(purpose text, claimed_identity text, key_id text, msg bytea, sig_hex text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
DECLARE k public.signing_key; parts text[];
BEGIN
  IF purpose IS NULL OR claimed_identity IS NULL OR key_id IS NULL OR msg IS NULL OR sig_hex IS NULL THEN RETURN false; END IF;
  IF sig_hex !~ '^[0-9a-f]{128}$' THEN RETURN false; END IF;
  parts := pg_catalog.regexp_match(key_id, '^([a-z][a-z-]*)\.ed25519\.([1-9][0-9]{0,8})$');
  IF parts IS NULL THEN RETURN false; END IF;
  SELECT * INTO k FROM public.signing_key sk WHERE sk.key_id = verify.key_id;
  IF NOT FOUND THEN RETURN false; END IF;
  IF k.status <> 'ACTIVE' OR k.algorithm <> 'ED25519' THEN RETURN false; END IF;
  IF k.identity <> claimed_identity OR k.identity <> parts[1] OR k.version <> parts[2]::integer THEN RETURN false; END IF;
  IF NOT ((purpose = 'receipt' AND k.identity = 'core-execution')
       OR (purpose = 'outcome-verdict' AND k.identity = 'core-outcome')
       OR (purpose = 'record-event' AND noema_sig.signing_identity(k.identity))) THEN RETURN false; END IF;
  IF pg_catalog.octet_length(k.public_key) <> 32 THEN RETURN false; END IF;
  RETURN pgsodium.crypto_sign_verify_detached(pg_catalog.decode(sig_hex, 'hex'), msg, k.public_key) IS TRUE;
END $$;
ALTER FUNCTION noema_sig.verify(text, text, text, bytea, text) OWNER TO noema_migrator;
REVOKE ALL ON FUNCTION noema_sig.verify(text, text, text, bytea, text) FROM PUBLIC;

-- D8: the database has no JCS implementation, so each signed table stores the exact bytes that were signed (without the domain tag).
ALTER TABLE public.execution_receipt ADD COLUMN signed_jcs bytea NOT NULL;
ALTER TABLE public.execution_receipt OWNER TO noema_migrator;
-- Grants that follow from the owner change and from the checks below.
GRANT SELECT ON public.grant_spend TO noema_migrator;
GRANT SELECT ON public.signing_key TO trust_rw, execution_rw, outcome_rw, record_append;

-- D7, D8, D12: what a receipt must be to be inserted.
CREATE FUNCTION public.execution_receipt_verify() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
DECLARE doc jsonb; gs public.grant_spend; msg bytea; want_ack jsonb;
BEGIN
  IF NEW.signed_jcs IS NULL OR NEW.sig_key_id IS NULL OR NEW.sig_value IS NULL THEN
    RAISE EXCEPTION 'signature not verified: the signed bytes or the signature are missing' USING ERRCODE = 'NS001';
  END IF;
  BEGIN
    doc := pg_catalog.convert_from(NEW.signed_jcs, 'UTF8')::jsonb;
  EXCEPTION WHEN others THEN
    RAISE EXCEPTION 'signature not verified: the signed bytes are not a JSON document' USING ERRCODE = 'NS001';
  END;
  IF jsonb_typeof(doc) <> 'object' OR doc ? 'sig' THEN
    RAISE EXCEPTION 'signature not verified: the signed document must be an object without sig' USING ERRCODE = 'NS001';
  END IF;
  -- The stored document is the signed one plus the signature member, and the signature member is the row's.
  IF NEW.json IS NULL OR (NEW.json - 'sig') IS DISTINCT FROM doc
     OR (NEW.json -> 'sig') IS DISTINCT FROM pg_catalog.jsonb_build_object('key_id', NEW.sig_key_id, 'value', NEW.sig_value) THEN
    RAISE EXCEPTION 'signature not verified: the stored document is not the signed one' USING ERRCODE = 'NS001';
  END IF;
  -- D7 check 1: every bound field equals the row's own column. (CASE is computed apart: PL/pgSQL ends an IF condition at the first THEN.)
  IF NEW.external_ack IS NULL THEN want_ack := 'null'::jsonb; ELSE want_ack := pg_catalog.to_jsonb(NEW.external_ack); END IF;
  IF (doc ->> 'receipt_id') IS DISTINCT FROM NEW.receipt_id
     OR (doc ->> 'tenant_id') IS DISTINCT FROM NEW.tenant_id
     OR (doc ->> 'grant_id') IS DISTINCT FROM NEW.grant_id
     OR jsonb_typeof(doc -> 'attempt') IS DISTINCT FROM 'number' OR (doc ->> 'attempt') IS DISTINCT FROM NEW.attempt::text
     OR (doc ->> 'idempotency_key') IS DISTINCT FROM NEW.idempotency_key
     OR (doc ->> 'logical_action') IS DISTINCT FROM NEW.logical_action
     OR (doc ->> 'execution_state') IS DISTINCT FROM NEW.execution_state
     OR (doc ->> 'dispatched_at') IS NULL OR (doc ->> 'dispatched_at')::timestamptz IS DISTINCT FROM NEW.dispatched_at
     OR (doc -> 'external_ack') IS DISTINCT FROM want_ack
     OR (doc ->> 'transport_result') IS DISTINCT FROM pg_catalog.split_part(NEW.raw_status, ':', 1)
     OR COALESCE(doc ->> 'correlation_id', '') = '' THEN
    RAISE EXCEPTION 'signature not verified: a signed field differs from the row' USING ERRCODE = 'NS001';
  END IF;
  -- D7 check 2: tenant, grant, attempt and idempotency key are the ones E8 recorded in grant_spend.
  SELECT * INTO gs FROM public.grant_spend s WHERE s.grant_id = NEW.grant_id;
  IF NOT FOUND OR gs.tenant_id <> NEW.tenant_id OR gs.attempt <> NEW.attempt OR gs.idempotency_key <> NEW.idempotency_key THEN
    RAISE EXCEPTION 'signature not verified: the receipt does not match the recorded spend' USING ERRCODE = 'NS001';
  END IF;
  -- D7 check 3 and D6: the key is core-execution's, and the signature is over the domain tag followed by the signed bytes.
  msg := pg_catalog.convert_to('noema/receipt/v1' || pg_catalog.chr(10), 'UTF8') || NEW.signed_jcs;
  IF NOT noema_sig.verify('receipt', 'core-execution', NEW.sig_key_id, msg, NEW.sig_value) THEN
    RAISE EXCEPTION 'signature not verified' USING ERRCODE = 'NS001';
  END IF;
  RETURN NEW;
END $$;
ALTER FUNCTION public.execution_receipt_verify() OWNER TO noema_migrator;
REVOKE ALL ON FUNCTION public.execution_receipt_verify() FROM PUBLIC;
CREATE TRIGGER execution_receipt_verify BEFORE INSERT ON public.execution_receipt FOR EACH ROW EXECUTE FUNCTION public.execution_receipt_verify();
ALTER TABLE public.execution_receipt ENABLE ALWAYS TRIGGER execution_receipt_verify;

-- D16: execution_rw loses INSERT on the table and inserts through this function. The trigger still verifies, whatever the path.
CREATE FUNCTION public.receipt_insert(
  p_receipt_id text, p_tenant_id text, p_grant_id text, p_attempt bigint, p_idempotency_key text, p_logical_action text,
  p_execution_state text, p_raw_status text, p_external_ack text, p_dispatched_at timestamptz, p_json jsonb,
  p_sig_key_id text, p_sig_value text, p_signed_jcs bytea, p_created_at timestamptz) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
DECLARE n bigint;
BEGIN
  INSERT INTO public.execution_receipt (receipt_id, tenant_id, grant_id, attempt, idempotency_key, logical_action, execution_state, raw_status,
                                        external_ack, dispatched_at, json, sig_key_id, sig_value, signed_jcs, created_at)
  VALUES (p_receipt_id, p_tenant_id, p_grant_id, p_attempt, p_idempotency_key, p_logical_action, p_execution_state, p_raw_status,
          p_external_ack, p_dispatched_at, p_json, p_sig_key_id, p_sig_value, p_signed_jcs, p_created_at)
  ON CONFLICT (grant_id, attempt) DO NOTHING;
  GET DIAGNOSTICS n = ROW_COUNT;
  RETURN n = 1;
END $$;
ALTER FUNCTION public.receipt_insert(text, text, text, bigint, text, text, text, text, text, timestamptz, jsonb, text, text, bytea, timestamptz) OWNER TO noema_migrator;
REVOKE ALL ON FUNCTION public.receipt_insert(text, text, text, bigint, text, text, text, text, text, timestamptz, jsonb, text, text, bytea, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.receipt_insert(text, text, text, bigint, text, text, text, text, text, timestamptz, jsonb, text, text, bytea, timestamptz) TO execution_rw;
REVOKE INSERT ON public.execution_receipt FROM execution_rw;
