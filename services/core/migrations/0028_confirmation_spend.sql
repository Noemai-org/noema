-- 0028: the single-use spend of a given confirmation (TRU-07 follow-up; EL's answer to Q-C on noema#572; PERD REQ-TRU-008, REQ-TRU-009, REQ-AUD-001;
-- INV-03, INV-17, INV-21, INV-25). Number assigned by the migration ledger on noema#183 (C1, 15 Nov 2026). Forward-only: never edited after merge.
-- No CREATE ROLE here (TestNoMigrationAfter0001CreatesARole).
--
-- A confirmation that was given authorises one decision. The decision that follows it (trust/recorder: Writer.Write) writes one row here, in the same
-- transaction as its Decision Record and its grants, and the database refuses a second row for the same confirmation event of the same tenant. A second
-- ALLOW that names a spent confirmation therefore cannot commit, however two evaluations are interleaved. The writer spends only when the decision row is
-- newly written (a retry of the same input returns the stored decision, A186, and reaches no insert here).
--
-- A spend names a recorded ALLOW decision of its own tenant, checked by a trigger (the way decision_grant is, 0012), and one decision follows at most one
-- confirmation. There is no foreign key to decision_record: a foreign key would turn the insert-only refusal of TRUNCATE on decision_record into another
-- error (0A000 instead of the trigger's), and the table's own tests pin that code.

CREATE TABLE confirmation_spend (
  tenant_id             text NOT NULL REFERENCES tenant (tenant_id),
  confirmation_event_id text NOT NULL CHECK (confirmation_event_id ~ '^cfe_[0-9A-Za-z]{2,32}$'),
  decision_id           text NOT NULL,
  -- the plan the confirmation was given for (A230): the writer refuses an ALLOW whose steps carry another, and the Record can show later which plan a spend was for
  plan_hash             text NOT NULL CHECK (plan_hash ~ '^sha256:[0-9a-f]{64}$'),
  spent_at              timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT confirmation_spend_event_key UNIQUE (tenant_id, confirmation_event_id),
  CONSTRAINT confirmation_spend_decision_key UNIQUE (tenant_id, decision_id)
);

-- Insert-only, like the Record and the decisions: a spend is never edited, deleted or truncated, by any role.
CREATE TRIGGER confirmation_spend_immutable BEFORE UPDATE OR DELETE ON confirmation_spend FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER confirmation_spend_no_truncate BEFORE TRUNCATE ON confirmation_spend FOR EACH STATEMENT EXECUTE FUNCTION reject_mutation();

REVOKE ALL ON confirmation_spend FROM PUBLIC;
GRANT SELECT, INSERT ON confirmation_spend TO trust_rw;   -- only Trust spends a confirmation, and only with its own decision

-- A write is bound to the tenant of the login that makes it (DEBT-04 option A; REQ-SEC-004, INV-11), exactly as for decision_record (0012): session_user is
-- looked up in public.tenant_login with the catalog first and pg_temp last in the search path. A superuser is not bound (no runtime login is one).
CREATE FUNCTION confirmation_spend_tenant_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  IF (SELECT r.rolsuper FROM pg_catalog.pg_roles r WHERE r.rolname = session_user::text) THEN
    RETURN NEW;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM public.tenant_login l
                 WHERE l.login_role = session_user::text AND l.power = 'trust' AND l.tenant_id = NEW.tenant_id) THEN
    RAISE EXCEPTION 'confirmation_spend: this login may not spend confirmations for tenant %', NEW.tenant_id USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION confirmation_spend_tenant_guard() FROM PUBLIC;
CREATE TRIGGER confirmation_spend_tenant_guard BEFORE INSERT ON confirmation_spend FOR EACH ROW EXECUTE FUNCTION confirmation_spend_tenant_guard();

-- Only an ALLOW decision of the spend's own tenant can spend a confirmation (REQ-TRU-008). The trigger runs as the inserting role, which is Trust and holds
-- SELECT on decision_record, so row security applies to its read. Its search_path is pinned to the catalog with pg_temp LAST and the table is schema-qualified,
-- so a login holding TEMPORARY cannot shadow decision_record with a temporary table holding an ALLOW row (the rule of decision_grant_requires_allow, 0012).
CREATE FUNCTION confirmation_spend_requires_allow() RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE decided text;
BEGIN
  SELECT d.outcome INTO decided FROM public.decision_record d WHERE d.decision_id = NEW.decision_id AND d.tenant_id = NEW.tenant_id;
  IF decided IS DISTINCT FROM 'ALLOW' THEN
    RAISE EXCEPTION 'confirmation_spend: decision % of tenant % is not a recorded ALLOW decision', NEW.decision_id, NEW.tenant_id
      USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER confirmation_spend_requires_allow BEFORE INSERT ON confirmation_spend FOR EACH ROW EXECUTE FUNCTION confirmation_spend_requires_allow();

-- Reads and writes are scoped to the login's tenant by row-level security on the same mapping as decision_record (decision_login_tenant(), 0012). The table's
-- owner is subject to the policies too (FORCE); a superuser bypasses them, and no runtime login is one.
ALTER TABLE confirmation_spend ENABLE ROW LEVEL SECURITY;
CREATE POLICY confirmation_spend_read ON confirmation_spend FOR SELECT TO trust_rw USING (tenant_id = decision_login_tenant());
CREATE POLICY confirmation_spend_write ON confirmation_spend FOR INSERT TO trust_rw WITH CHECK (tenant_id = decision_login_tenant());
ALTER TABLE confirmation_spend FORCE ROW LEVEL SECURITY;
