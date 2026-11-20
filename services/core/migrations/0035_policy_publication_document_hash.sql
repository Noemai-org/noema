-- 0035: the hash of the policy document on the publication row (TRU-02, noema#771). REQ-TRU-005, REQ-TRU-004; INV-09, INV-21; ruling on noema#771 (interim, A-number not yet assigned).
-- Number 0035: the next free number after 0030 (noema#716), 0031 (outbox), 0032 (confirmation store, noema#756), 0033 (resolution epoch, noema#769) and 0034 (decision key, noema#759); the
-- Steward registers it on noema#183, and if merge order changes the later PR renumbers (ruling A34). Forward-only: 0015 and 0024 are merged and never edited. No CREATE ROLE.
--
-- A publication holds the policy_id and the version, not the document (the store of documents is DEBT-23, still open). So a different document under the same version could not be told from the
-- first. document_hash is the hash of the document (policy.DocumentHash in trust/policy: domain-tagged SHA-256 of the RFC 8785 form, written sha256: and 64 lower-case hex digits). The command
-- that publishes computes it; the loader that reads the document again recomputes it and refuses a document that is not the published one.
--
-- The column is NULL for a publication made before this migration and stays so: a publication is insert-only (policy_publication_immutable), the documents of old publications are not
-- stored, and a hash cannot be made up for them. Every NEW publication must carry one: the form is a CHECK, and the presence is a BEFORE INSERT trigger rather than NOT NULL, because
-- NOT NULL would refuse the old rows. The guard of 0015 (the login mapped to the tenant, the version higher than the active one), the row-level security, the immutability and the
-- tenant and per-power grants are unchanged; the grants of the table cover the new column.

ALTER TABLE policy_publication ADD COLUMN document_hash text NULL;

ALTER TABLE policy_publication ADD CONSTRAINT policy_publication_document_hash_form
  CHECK (document_hash IS NULL OR document_hash ~ '^sha256:[0-9a-f]{64}$');

CREATE FUNCTION policy_publication_require_document_hash() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  IF NEW.document_hash IS NULL THEN
    RAISE EXCEPTION 'policy_publication: a new publication needs the hash of its policy document' USING ERRCODE = 'not_null_violation';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION policy_publication_require_document_hash() FROM PUBLIC;

-- The name sorts after policy_publication_guard, so the guard's login and version checks run first (BEFORE triggers of one table fire in name order).
CREATE TRIGGER policy_publication_guard_document_hash BEFORE INSERT ON policy_publication FOR EACH ROW EXECUTE FUNCTION policy_publication_require_document_hash();
