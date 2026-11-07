-- 0019: context_snapshot and the evidence_verify login's grants (EVD-03 slice 2; PERD 21.3, 38.2). REQ-CTX-004, INV-17, INV-11, REQ-SEC-004,
-- DEC-2026-10-16 C1, D9. Forward-only: never edited after merge (AGENTS.md section 7). The number is provisional until the merge ledger (noema#183)
-- assigns it; the migrator applies by name and accepts a gap.
--
-- A sealed snapshot is an immutable set of evidence IDs and the hash over their (evidence_id, payload_hash) pairs. The hash is recomputed here, in
-- the database, from the stored evidence rows of the writer's own tenant: Trust cannot store a hash it did not compute, a set it did not read, or an
-- empty set (INV-17). Written by the tenant's Trust login only; read by the same login (row-level security) and, across tenants, by evidence_verify.
--
-- No role is created here: evidence_verify is created by deploy/compose/postgres/init.sh (a LOGIN role that is a member of no role). A missing role
-- stops the migration with a message that says where to create it.

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'evidence_verify' AND rolcanlogin) THEN
    RAISE EXCEPTION 'the login role evidence_verify does not exist: create it in deploy/compose/postgres/init.sh before migrating';
  END IF;
END $$;

-- The evidence_set_hash of PERD 21.3 step 2 and the golden vectors (CON-04): the pairs sorted by the UTF-8 bytes of evidence_id, each written as
-- LP(evidence_id) LP(payload_hash), LP being a 4-byte big-endian length and then the bytes; SHA-256; the prefix "sha256:". An empty or repeated
-- evidence_id, a payload_hash that is not "sha256:" and 64 lowercase hex characters, and arrays of different lengths are errors. The empty set
-- hashes to the SHA-256 of zero bytes, which is a fact about the function, not a permission (the table below never accepts it).
CREATE FUNCTION evidence_set_hash_of(p_ids text[], p_hashes text[]) RETURNS text
LANGUAGE plpgsql IMMUTABLE SET search_path = pg_catalog, pg_temp AS $$
DECLARE v_bytes bytea;
BEGIN
  IF coalesce(array_length(p_ids, 1), 0) <> coalesce(array_length(p_hashes, 1), 0) OR array_ndims(p_ids) > 1 OR array_ndims(p_hashes) > 1 THEN
    RAISE EXCEPTION 'evidence_set_hash_of: the arrays differ in length or are not one-dimensional' USING ERRCODE = 'data_exception';
  END IF;
  IF EXISTS (SELECT 1 FROM unnest(p_ids) AS i WHERE i IS NULL OR i = '')
     OR EXISTS (SELECT 1 FROM unnest(p_hashes) AS h WHERE h IS NULL OR h !~ '^sha256:[0-9a-f]{64}$') THEN
    RAISE EXCEPTION 'evidence_set_hash_of: an evidence_id is empty or a payload_hash is not sha256: and 64 lowercase hex characters' USING ERRCODE = 'data_exception';
  END IF;
  IF (SELECT count(DISTINCT i) FROM unnest(p_ids) AS i) <> coalesce(array_length(p_ids, 1), 0) THEN
    RAISE EXCEPTION 'evidence_set_hash_of: an evidence_id appears more than once' USING ERRCODE = 'data_exception';
  END IF;
  SELECT coalesce(string_agg(int4send(octet_length(convert_to(t.i, 'UTF8'))) || convert_to(t.i, 'UTF8')
                             || int4send(octet_length(convert_to(t.h, 'UTF8'))) || convert_to(t.h, 'UTF8'), ''::bytea
                             ORDER BY convert_to(t.i, 'UTF8')), ''::bytea)
    INTO v_bytes FROM unnest(p_ids, p_hashes) AS t (i, h);
  RETURN 'sha256:' || encode(sha256(v_bytes), 'hex');
END $$;
REVOKE ALL ON FUNCTION evidence_set_hash_of(text[], text[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION evidence_set_hash_of(text[], text[]) TO trust_rw;

CREATE TABLE context_snapshot (
  snapshot_id                   text PRIMARY KEY CHECK (snapshot_id ~ '^evs_[0-9A-Za-z]{2,32}$'),
  tenant_id                     text NOT NULL REFERENCES tenant (tenant_id),
  correlation_id                text NOT NULL CHECK (correlation_id ~ '^corr_[0-9A-Za-z]{2,32}$'),
  -- Sorted ascending by bytes, no duplicates (C1 step 8); at least one: Trust never issues a grant over an empty evidence set (INV-17).
  evidence_ids                  text[] NOT NULL CHECK (array_ndims(evidence_ids) = 1 AND cardinality(evidence_ids) >= 1),
  reconciliation_id             text NOT NULL CHECK (reconciliation_id ~ '^rec_[0-9A-Za-z]{2,32}$'),
  reconciliation_classification text NOT NULL CHECK (reconciliation_classification ~ '^[A-Z][A-Z0-9_]*$'),
  evidence_set_hash             text NOT NULL CHECK (evidence_set_hash ~ '^sha256:[0-9a-f]{64}$'
                                  AND evidence_set_hash <> 'sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'),
  sealed_at                     timestamptz NOT NULL
);
CREATE INDEX context_snapshot_tenant_correlation_idx ON context_snapshot (tenant_id, correlation_id);
CREATE TRIGGER context_snapshot_immutable BEFORE UPDATE OR DELETE ON context_snapshot FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER context_snapshot_no_truncate BEFORE TRUNCATE ON context_snapshot FOR EACH STATEMENT EXECUTE FUNCTION reject_mutation();

-- A snapshot is written for the tenant of the Trust login that writes it (session_user, which a caller cannot change; public.tenant_login is
-- schema-qualified and pg_temp is last in the search path, as in 0008), its ids are sorted and unique, every id names an evidence item of that same
-- tenant, and the stored hash is the one the database computes from those items' payload hashes. Anything else is refused.
CREATE FUNCTION context_snapshot_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
DECLARE v_hashes text[]; v_found integer;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM public.tenant_login l
                 WHERE l.login_role = session_user::text AND l.power = 'trust' AND l.tenant_id = NEW.tenant_id) THEN
    RAISE EXCEPTION 'context_snapshot: this login may not seal a snapshot for tenant %', NEW.tenant_id USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF array_position(NEW.evidence_ids, NULL) IS NOT NULL THEN
    RAISE EXCEPTION 'context_snapshot: an evidence id is NULL' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  IF NEW.evidence_ids <> ARRAY(SELECT i FROM unnest(NEW.evidence_ids) AS i ORDER BY convert_to(i, 'UTF8'))
     OR (SELECT count(DISTINCT i) FROM unnest(NEW.evidence_ids) AS i) <> cardinality(NEW.evidence_ids) THEN
    RAISE EXCEPTION 'context_snapshot: the evidence ids are not sorted ascending by bytes and unique' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  SELECT count(*), array_agg(e.payload_hash ORDER BY convert_to(e.evidence_id, 'UTF8')) INTO v_found, v_hashes
    FROM public.evidence_item e WHERE e.tenant_id = NEW.tenant_id AND e.evidence_id = ANY (NEW.evidence_ids);
  IF v_found <> cardinality(NEW.evidence_ids) THEN
    RAISE EXCEPTION 'context_snapshot: an evidence id does not name an evidence item of tenant %', NEW.tenant_id USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  IF NEW.evidence_set_hash IS DISTINCT FROM public.evidence_set_hash_of(NEW.evidence_ids, v_hashes) THEN
    RAISE EXCEPTION 'context_snapshot: the evidence_set_hash is not the hash of the stored items' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION context_snapshot_guard() FROM PUBLIC;
CREATE TRIGGER context_snapshot_guard BEFORE INSERT ON context_snapshot FOR EACH ROW EXECUTE FUNCTION context_snapshot_guard();

ALTER TABLE context_snapshot ENABLE ROW LEVEL SECURITY;
ALTER TABLE context_snapshot FORCE ROW LEVEL SECURITY;
CREATE POLICY context_snapshot_tenant ON context_snapshot FOR ALL TO trust_rw
  USING (tenant_id = evidence_login_tenant()) WITH CHECK (tenant_id = evidence_login_tenant());

-- context_snapshot_guard() is SECURITY DEFINER and reads evidence_item as its owner, and evidence_item forces row-level security (0008), which binds the
-- owner too: with no policy for the owner, a non-superuser migration login (the Compose one) sees no evidence row and every snapshot is refused (Security
-- Reviewer, noema#268). The owner may read the items of the tenant of the login that is sealing, as evidence_login_tenant() names it, and no others.
CREATE POLICY evidence_item_definer_read ON evidence_item FOR SELECT TO CURRENT_USER USING (tenant_id = evidence_login_tenant());

REVOKE ALL ON context_snapshot FROM PUBLIC;
GRANT SELECT, INSERT ON context_snapshot TO trust_rw;

-- D9: the nightly evidence recompute logs in as evidence_verify. It reads context_snapshot and evidence_item across tenants and holds nothing else:
-- one SELECT policy per table (the tables force row-level security, so a grant alone would show it no row), no payload table, no Record, no
-- function, no membership. It cannot write (no privilege, and the insert-only triggers).
GRANT USAGE ON SCHEMA public TO evidence_verify;
GRANT SELECT ON context_snapshot, evidence_item TO evidence_verify;
CREATE POLICY context_snapshot_evidence_verify ON context_snapshot FOR SELECT TO evidence_verify USING (true);
CREATE POLICY evidence_item_evidence_verify ON evidence_item FOR SELECT TO evidence_verify USING (true);
