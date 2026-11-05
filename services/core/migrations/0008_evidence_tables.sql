-- 0008: evidence_payload and evidence_item (PERD 38.2, SoT 43.5). EVD-01 slice 2a.
-- REQ-CTX-001 (immutable evidence), INV-10, INV-19, INV-11. Forward-only: never edited after merge (AGENTS.md section 7).
-- Migrations apply by name; 0007 belongs to the Outcome verdict migration (OUT-01), so a gap here is expected.
--
-- Written by the Evidence Layer only (evidence_rw, on the tenant's own login); read by Trust and Outcome. Both tables are insert-only (a trigger, not a
-- convention), carry the tenant on every row, and an item points at a payload of its own tenant by a composite key.

CREATE TABLE evidence_payload (
  payload_ref text PRIMARY KEY,
  tenant_id   text NOT NULL REFERENCES tenant (tenant_id),
  json        jsonb NOT NULL,
  raw_hash    text NOT NULL CHECK (raw_hash ~ '^sha256:[0-9a-f]{64}$'),
  sensitivity text NOT NULL CHECK (sensitivity ~ '^S[0-9]_[a-z][a-z0-9_]*$'),
  created_at  timestamptz NOT NULL,
  CONSTRAINT evidence_payload_tenant_ref_key UNIQUE (tenant_id, payload_ref)
);
CREATE TRIGGER evidence_payload_immutable BEFORE UPDATE OR DELETE ON evidence_payload FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER evidence_payload_no_truncate BEFORE TRUNCATE ON evidence_payload FOR EACH STATEMENT EXECUTE FUNCTION reject_mutation();

CREATE TABLE evidence_item (
  evidence_id           text PRIMARY KEY,
  tenant_id             text NOT NULL REFERENCES tenant (tenant_id),
  correlation_id        text NOT NULL,
  source                text NOT NULL,
  source_record_id      text NOT NULL CHECK (source_record_id <> ''),
  read_by               text NOT NULL CHECK (read_by IN ('TRUST', 'OUTCOME', 'RESOLUTION', 'EXECUTION_REVALIDATION')),
  read_at               timestamptz NOT NULL,
  effective_at          timestamptz NOT NULL,
  freshness_sla_seconds integer NOT NULL CHECK (freshness_sla_seconds >= 1),
  authority_class       text NOT NULL CHECK (authority_class = 'NONE' OR authority_class ~ '^(AUTHORITATIVE_FOR|CORROBORATING_FOR):.+'),
  resource_scope        text NOT NULL,
  principal_scope       text NOT NULL,
  sensitivity           text NOT NULL CHECK (sensitivity ~ '^S[0-9]_[a-z][a-z0-9_]*$'),
  fact_status           text NOT NULL CHECK (fact_status IN ('KNOWN', 'INFERRED', 'AMBIGUOUS', 'UNAVAILABLE', 'STALE', 'CONTRADICTORY', 'UNKNOWN', 'UNAUTHORIZED')),
  payload_hash          text NOT NULL CHECK (payload_hash ~ '^sha256:[0-9a-f]{64}$'),
  payload_ref           text NOT NULL,
  created_at            timestamptz NOT NULL,
  CONSTRAINT evidence_item_payload_fkey FOREIGN KEY (tenant_id, payload_ref) REFERENCES evidence_payload (tenant_id, payload_ref)
);
CREATE INDEX evidence_item_tenant_correlation_idx ON evidence_item (tenant_id, correlation_id);
CREATE INDEX evidence_item_tenant_source_record_idx ON evidence_item (tenant_id, source, source_record_id, read_at);
CREATE TRIGGER evidence_item_immutable BEFORE UPDATE OR DELETE ON evidence_item FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER evidence_item_no_truncate BEFORE TRUNCATE ON evidence_item FOR EACH STATEMENT EXECUTE FUNCTION reject_mutation();

-- A write is bound to the tenant of the login that makes it (DEBT-04 option A; REQ-SEC-004, INV-11): session_user, which a caller
-- cannot change, is looked up in tenant_login. A login with no mapping, a login of another power and a row naming another
-- tenant are refused. A foreign key proves only that a tenant exists, not that the caller belongs to it.
CREATE FUNCTION evidence_row_tenant_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM tenant_login l
                 WHERE l.login_role = session_user::text AND l.power = 'evidence' AND l.tenant_id = NEW.tenant_id) THEN
    RAISE EXCEPTION '%: this login may not write evidence for tenant %', TG_TABLE_NAME, NEW.tenant_id USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION evidence_row_tenant_guard() FROM PUBLIC;
CREATE TRIGGER evidence_payload_tenant_guard BEFORE INSERT ON evidence_payload FOR EACH ROW EXECUTE FUNCTION evidence_row_tenant_guard();
CREATE TRIGGER evidence_item_tenant_guard BEFORE INSERT ON evidence_item FOR EACH ROW EXECUTE FUNCTION evidence_row_tenant_guard();

-- Reads are bound to the login's tenant by row-level security, on the same session_user mapping (Codex review of noema#167): a table-wide
-- SELECT would let any tenant's Trust, Outcome or Evidence login read every tenant's evidence. A session with no tenant mapping sees no row.
CREATE FUNCTION evidence_login_tenant() RETURNS text
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, public AS $$
  SELECT l.tenant_id FROM tenant_login l WHERE l.login_role = session_user::text
$$;
REVOKE ALL ON FUNCTION evidence_login_tenant() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION evidence_login_tenant() TO evidence_rw, trust_rw, outcome_rw;
ALTER TABLE evidence_payload ENABLE ROW LEVEL SECURITY;
ALTER TABLE evidence_item ENABLE ROW LEVEL SECURITY;
CREATE POLICY evidence_payload_tenant ON evidence_payload FOR ALL TO evidence_rw, trust_rw, outcome_rw
  USING (tenant_id = evidence_login_tenant()) WITH CHECK (tenant_id = evidence_login_tenant());
CREATE POLICY evidence_item_tenant ON evidence_item FOR ALL TO evidence_rw, trust_rw, outcome_rw
  USING (tenant_id = evidence_login_tenant()) WITH CHECK (tenant_id = evidence_login_tenant());

REVOKE ALL ON evidence_payload, evidence_item FROM PUBLIC;
GRANT SELECT, INSERT ON evidence_payload, evidence_item TO evidence_rw;
GRANT SELECT ON evidence_payload, evidence_item TO trust_rw, outcome_rw;
