-- 0003: the one read path into the Record, for reconstruction and chain verification.
-- Tasks REC-03 (reconstruction, REQ-AUD-003) and REC-05 (verification, REQ-AUD-005); REQ-AUD-001 (no power holds a
-- privilege on the Record tables). Approved by the Architect on 1 Nov 2026 (escalation E1 of REC-03).
-- Forward-only: this file is never edited after merge (AGENTS.md section 7).
--
-- Numbered 0003 on purpose: 0002 is held by noema#94 (EXE-04). platform/db.Migrate applies every unapplied file in
-- name order and has no gap check, so on a database migrated to 0003 a later 0002 is simply applied after it. This
-- file depends only on 0001, and the 0002 of noema#94 grants and revokes nothing, so the two orders give the same
-- privileges (platform/db and record/chain tests prove both orders).

-- The role record_read is created by the deployment (deploy/compose/postgres/init.sh), never by a migration: the migrating
-- role cannot create roles. This file only checks that it exists.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'record_read') THEN
    RAISE EXCEPTION 'the role record_read does not exist: create it in deploy/compose/postgres/init.sh before migrating';
  END IF;
END $$;

-- One tenant's whole chain, in chain order, exactly as stored: the verifier needs every column it recomputes, and
-- reconstruction serves only what it has just verified in the same snapshot. Owned by record_append (the owner of
-- the Record tables), so the caller needs no privilege on the tables themselves. STABLE: it reads and never writes.
CREATE FUNCTION record_read_chain(p_tenant text) RETURNS TABLE (
  seq bigint, event_id text, prev_hash text, hash text, written_by text, actor text, event_type text, object_ref text,
  payload_hash text, correlation_id text, schema_version text, body text, payload text
)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, public AS $$
  SELECT e.seq, e.event_id, e.prev_hash, e.hash, e.written_by, e.actor, e.event_type, e.object_ref, e.payload_hash,
         e.correlation_id, e.schema_version, e.body::text, e.payload::text
  FROM record_event e WHERE e.tenant_id = p_tenant ORDER BY e.seq
$$;

ALTER FUNCTION record_read_chain(text) OWNER TO record_append;

-- Only record_read may call it. No power role is given it; record_read is given nothing else on the Record.
REVOKE ALL ON FUNCTION record_read_chain(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION record_read_chain(text) TO record_read;
GRANT USAGE ON SCHEMA public TO record_read;
