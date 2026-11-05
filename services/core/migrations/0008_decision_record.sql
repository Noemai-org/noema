-- 0008: the Decision Record, and the rule that a grant exists only for an ALLOW decision of the same tenant (TRU-05; PERD 38.2).
-- REQ-TRU-001, REQ-TRU-008, REQ-DATA-001; INV-03, INV-16. Number decided by the Architect on 4 Nov 2026 (after noema#136's 0007).
-- Forward-only: this file is never edited after merge. No CREATE ROLE here (TestNoMigrationAfter0001CreatesARole).

CREATE TABLE decision_record (
  decision_id             text PRIMARY KEY,
  tenant_id               text NOT NULL REFERENCES tenant (tenant_id),
  json                    jsonb NOT NULL,
  outcome                 text NOT NULL CHECK (outcome IN ('ALLOW', 'CLARIFY', 'CONFIRM', 'WAIT', 'ESCALATE', 'REJECT', 'UNKNOWN')),
  disposition_decision_id text REFERENCES decision_record (decision_id),
  intent_hash             text NOT NULL CHECK (intent_hash ~ '^sha256:[0-9a-f]{64}$'),
  policy_versions_json    jsonb NOT NULL,
  sig_key_id              text NOT NULL,
  sig_value               text NOT NULL,
  created_at              timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX decision_record_tenant_outcome_idx ON decision_record (tenant_id, outcome);
CREATE INDEX decision_record_disposition_idx ON decision_record (disposition_decision_id) WHERE disposition_decision_id IS NOT NULL;

-- Insert-only, like the Record: a decision is never edited, deleted or truncated, by any role.
CREATE TRIGGER decision_record_immutable BEFORE UPDATE OR DELETE ON decision_record FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER decision_record_no_truncate BEFORE TRUNCATE ON decision_record FOR EACH STATEMENT EXECUTE FUNCTION reject_mutation();

REVOKE ALL ON decision_record FROM PUBLIC;
GRANT SELECT, INSERT ON decision_record TO trust_rw;   -- only Trust writes decisions
GRANT SELECT ON decision_record TO execution_rw;       -- Execution checks the decision a grant came from
GRANT SELECT ON decision_record TO outcome_rw;

-- The Decision Record comes first, and only an ALLOW decision of the grant's own tenant can have a grant (REQ-TRU-008: grants where the
-- decision is not ALLOW: 0). The trigger runs as the inserting role, which is Trust and holds SELECT on decision_record.
CREATE FUNCTION decision_grant_requires_allow() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE decided text;
BEGIN
  SELECT d.outcome INTO decided FROM decision_record d WHERE d.decision_id = NEW.decision_id AND d.tenant_id = NEW.tenant_id;
  IF decided IS DISTINCT FROM 'ALLOW' THEN
    RAISE EXCEPTION 'decision_grant: decision % of tenant % is not a recorded ALLOW decision', NEW.decision_id, NEW.tenant_id
      USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER decision_grant_requires_allow BEFORE INSERT ON decision_grant FOR EACH ROW EXECUTE FUNCTION decision_grant_requires_allow();
