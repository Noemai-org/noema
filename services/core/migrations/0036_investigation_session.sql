-- 0036: the investigation names the role and the authn reference of the verified session that started it (STM-05; A441, interim, relayed, not yet committed; A149, A333). REQ-RCV-001, REQ-STM-001, REQ-SEC-004; INV-12, INV-30.
-- Number 0036: the next free number after 0035 (noema#771) on main; noema#756 holds 0032. If merge order changes, the later PR renumbers (ruling A34). Forward-only: 0025 and 0029 are merged and never edited. No CREATE ROLE.
--
-- Recovery (noema#789) builds its session from the stored row, so the row must say who started the investigation and as what. Both values come from the verified session at creation (INV-12), never from a
-- request or a model. principal_role is one of the four roles of A1; authn_ref is the reference of the session, never the token (A149): the SessionId shape of contracts/schemas/common.v1.json.
--
-- Legacy rows: a row written before this migration has neither value and none is invented for it. Both stay NULL. A NOT NULL with a marker value and a NOT VALID CHECK (the pattern of 0029) would make an
-- UPDATE of a legacy row fail, so a legacy investigation could never move. The presence on every NEW row is a BEFORE INSERT trigger (the pattern of 0035); the form and the both-or-neither rule are CHECKs.
-- The reader that finds NULL treats the row as having no recorded session and recovery refuses it.
--
-- Unchanged: tenant_id, row-level security with FORCE, the per-power roles and the grants of 0025. Neither column is in the orchestrator's column-limited UPDATE grant of 0025, so neither changes after the insert.
-- No index: no query path selects by either column; the reads are by (tenant_id, investigation_id), the primary key.

ALTER TABLE investigation ADD COLUMN principal_role text NULL;
ALTER TABLE investigation ADD COLUMN authn_ref text NULL;

ALTER TABLE investigation ADD CONSTRAINT investigation_principal_role_form CHECK (principal_role IS NULL OR principal_role IN ('customer', 'ops', 'approver', 'audit'));
ALTER TABLE investigation ADD CONSTRAINT investigation_authn_ref_form CHECK (authn_ref IS NULL OR authn_ref ~ '^ses_[0-9A-Za-z]{2,32}$');
ALTER TABLE investigation ADD CONSTRAINT investigation_session_both_or_neither CHECK ((principal_role IS NULL) = (authn_ref IS NULL));

CREATE FUNCTION investigation_require_session() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  IF NEW.principal_role IS NULL OR NEW.authn_ref IS NULL THEN
    RAISE EXCEPTION 'investigation: a new investigation needs the role and the authn reference of the session that started it' USING ERRCODE = 'not_null_violation';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION investigation_require_session() FROM PUBLIC;

-- The name sorts after investigation_guard, so the guard's state and version checks run first (BEFORE triggers of one table fire in name order).
CREATE TRIGGER investigation_guard_session BEFORE INSERT ON investigation FOR EACH ROW EXECUTE FUNCTION investigation_require_session();
