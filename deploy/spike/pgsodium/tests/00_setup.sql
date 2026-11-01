-- SEC-12 spike schema. Run by the bootstrap superuser. SYNTHETIC. Mirrors the trust model of the evaluation (section 3):
-- the key table, verifier and trigger belong to a migration role; the runtime role holds no ownership.
CREATE ROLE spike_migration NOLOGIN;
CREATE ROLE execution_rw LOGIN;
CREATE EXTENSION pgsodium;                       -- T1: no shared_preload_libraries entry
GRANT USAGE ON SCHEMA pgsodium TO spike_migration;
GRANT EXECUTE ON FUNCTION pgsodium.crypto_sign_verify_detached(bytea, bytea, bytea) TO spike_migration;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
CREATE SCHEMA spike AUTHORIZATION spike_migration;
GRANT USAGE ON SCHEMA spike TO execution_rw;
\i /tests/vectors.sql
GRANT USAGE ON SCHEMA public TO spike_migration;
GRANT SELECT ON public.spike_key TO spike_migration;
GRANT USAGE ON SCHEMA public TO execution_rw;
GRANT SELECT ON public.spike_receipt_sig TO execution_rw;

SET ROLE spike_migration;
CREATE TABLE spike.trusted_key (
  key_id text PRIMARY KEY,
  power text NOT NULL DEFAULT 'execution',
  public_key bytea NOT NULL CHECK (octet_length(public_key) = 32),
  valid_from timestamptz NOT NULL,
  valid_to timestamptz,
  revoked_at timestamptz);
CREATE TABLE spike.execution_receipt (
  tenant_id text NOT NULL, grant_id text NOT NULL, attempt int NOT NULL, state text NOT NULL,
  key_id text NOT NULL, signature bytea NOT NULL,
  PRIMARY KEY (tenant_id, grant_id, attempt));

CREATE FUNCTION spike.verify_ed25519(sig bytea, msg bytea, pk bytea) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  IF sig IS NULL OR msg IS NULL OR pk IS NULL THEN
    RAISE EXCEPTION 'verify_ed25519: NULL argument' USING ERRCODE = 'SG001';
  END IF;
  IF octet_length(sig) <> 64 THEN
    RAISE EXCEPTION 'verify_ed25519: signature must be 64 bytes' USING ERRCODE = 'SG001';
  END IF;
  IF octet_length(pk) <> 32 THEN
    RAISE EXCEPTION 'verify_ed25519: public key must be 32 bytes' USING ERRCODE = 'SG001';
  END IF;
  RETURN pgsodium.crypto_sign_verify_detached(sig, msg, pk) IS TRUE;
END $$;

CREATE FUNCTION spike.verify_receipt() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
DECLARE k spike.trusted_key; msg bytea;
BEGIN
  SELECT * INTO k FROM spike.trusted_key
   WHERE key_id = NEW.key_id AND power = 'execution' AND revoked_at IS NULL
     AND valid_from <= now() AND (valid_to IS NULL OR now() < valid_to);
  IF NOT FOUND THEN
    RAISE EXCEPTION 'receipt key % is not an active trusted key', NEW.key_id USING ERRCODE = 'SG002';
  END IF;
  msg := convert_to(format('v0|%s|%s|%s|%s|%s', NEW.tenant_id, NEW.grant_id, NEW.attempt, NEW.state, NEW.key_id), 'UTF8');
  IF NOT spike.verify_ed25519(NEW.signature, msg, k.public_key) THEN
    RAISE EXCEPTION 'receipt signature does not verify' USING ERRCODE = 'SG003';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER verify_receipt BEFORE INSERT ON spike.execution_receipt
  FOR EACH ROW EXECUTE FUNCTION spike.verify_receipt();
ALTER TABLE spike.execution_receipt ENABLE ALWAYS TRIGGER verify_receipt;
INSERT INTO spike.trusted_key (key_id, public_key, valid_from, valid_to, revoked_at)
  SELECT key_id, public_key, valid_from, valid_to, revoked_at FROM public.spike_key;
RESET ROLE;
-- The runtime role may insert receipts (the verifier decides) but owns nothing and cannot read the key table's writes.
GRANT INSERT ON spike.execution_receipt TO execution_rw;
GRANT SELECT ON spike.trusted_key TO execution_rw;
