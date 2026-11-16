-- 0028: the confirmations an ALLOW may follow, and their single-use spend (TRU-07 follow-up; EL's answer to Q-C on noema#572; the Security Reviewer's blocking 1 on noema#572; rulings
-- A230 and A233, interim; PERD REQ-TRU-008, REQ-TRU-009, REQ-AUD-001; INV-03, INV-17, INV-21, INV-25). Number assigned by the migration ledger on noema#183 (C1, 15 Nov 2026).
-- Forward-only: never edited after merge. No CREATE ROLE here (TestNoMigrationAfter0001CreatesARole).
--
-- confirmation_resolution holds an accepted confirmation that resolved to a fresh evaluation (trust/confirm: PROCEED_TO_FRESH_EVALUATION): its event ID, its request ID, the BINDING HASH the
-- request was given for (SoT 43.2a: the principal, the intent hash, the capability, the target, the parameters hash, the evidence-set hash, the plan hash and the policy version) and its
-- expiry. It is written by the confirmation store after trust/confirm.Resolve, once, and never changed.
--
-- confirmation_spend holds the use of such a confirmation by the ALLOW it authorised. The writer (trust/recorder: Writer.Write) inserts one row in the same transaction as the Decision Record,
-- its grants and its events, and the database refuses the row unless (1) the confirmation exists for the tenant, (2) it has not expired by the database's clock, (3) its binding hash is the
-- one the writer computed from the ALLOW's own step, (4) the decision is a recorded ALLOW of the same tenant, and (5) neither the confirmation event nor the decision has a spend already. A
-- second ALLOW that names a spent, unknown, expired or other tenant's confirmation, or one given for another action, cannot commit however two evaluations interleave. The writer spends only
-- when the decision row is newly written (a retry of the same input returns the stored decision, A186, and reaches no insert here).
--
-- No foreign key to decision_record or confirmation_resolution: a foreign key would turn the insert-only refusal of TRUNCATE on decision_record into another error (0A000 instead of the
-- trigger's), and the table's own tests pin that code. The checks are triggers, as for decision_grant (0012).

CREATE TABLE confirmation_resolution (
  tenant_id               text NOT NULL REFERENCES tenant (tenant_id),
  confirmation_event_id   text NOT NULL CHECK (confirmation_event_id ~ '^cfe_[0-9A-Za-z]{2,32}$'),
  confirmation_request_id text NOT NULL CHECK (confirmation_request_id ~ '^cfr_[0-9A-Za-z]{2,32}$'),
  binding_hash            text NOT NULL CHECK (binding_hash ~ '^sha256:[0-9a-f]{64}$'),
  accepted_at             timestamptz NOT NULL,
  expires_at              timestamptz NOT NULL,
  recorded_at             timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT confirmation_resolution_pkey PRIMARY KEY (tenant_id, confirmation_event_id),
  -- a request is answered once
  CONSTRAINT confirmation_resolution_request_key UNIQUE (tenant_id, confirmation_request_id),
  CONSTRAINT confirmation_resolution_expiry_after_acceptance CHECK (expires_at >= accepted_at)
);

CREATE TABLE confirmation_spend (
  tenant_id             text NOT NULL REFERENCES tenant (tenant_id),
  confirmation_event_id text NOT NULL CHECK (confirmation_event_id ~ '^cfe_[0-9A-Za-z]{2,32}$'),
  decision_id           text NOT NULL,
  -- the plan the confirmation was given for (A230): the writer refuses an ALLOW whose step carries another, and the Record can show later which plan a spend was for
  plan_hash             text NOT NULL CHECK (plan_hash ~ '^sha256:[0-9a-f]{64}$'),
  -- the binding the writer computed from the ALLOW's own step; the trigger below requires it to be the confirmation's
  binding_hash          text NOT NULL CHECK (binding_hash ~ '^sha256:[0-9a-f]{64}$'),
  spent_at              timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT confirmation_spend_event_key UNIQUE (tenant_id, confirmation_event_id),
  CONSTRAINT confirmation_spend_decision_key UNIQUE (tenant_id, decision_id)
);

-- Insert-only, like the Record and the decisions: a row is never edited, deleted or truncated, by any role.
CREATE TRIGGER confirmation_resolution_immutable BEFORE UPDATE OR DELETE ON confirmation_resolution FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER confirmation_resolution_no_truncate BEFORE TRUNCATE ON confirmation_resolution FOR EACH STATEMENT EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER confirmation_spend_immutable BEFORE UPDATE OR DELETE ON confirmation_spend FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER confirmation_spend_no_truncate BEFORE TRUNCATE ON confirmation_spend FOR EACH STATEMENT EXECUTE FUNCTION reject_mutation();

REVOKE ALL ON confirmation_resolution FROM PUBLIC;
REVOKE ALL ON confirmation_spend FROM PUBLIC;
GRANT SELECT, INSERT ON confirmation_resolution TO trust_rw;   -- Trust records a confirmation it resolved
GRANT SELECT, INSERT ON confirmation_spend TO trust_rw;        -- only Trust spends a confirmation, and only with its own decision

-- A write is bound to the tenant of the login that makes it (DEBT-04 option A; REQ-SEC-004, INV-11), exactly as for decision_record (0012): session_user is looked up in public.tenant_login
-- with the catalog first and pg_temp last in the search path. A superuser is not bound (no runtime login is one). One function for both tables.
CREATE FUNCTION confirmation_tenant_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  IF (SELECT r.rolsuper FROM pg_catalog.pg_roles r WHERE r.rolname = session_user::text) THEN
    RETURN NEW;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM public.tenant_login l
                 WHERE l.login_role = session_user::text AND l.power = 'trust' AND l.tenant_id = NEW.tenant_id) THEN
    RAISE EXCEPTION '%: this login may not write for tenant %', TG_TABLE_NAME, NEW.tenant_id USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION confirmation_tenant_guard() FROM PUBLIC;
CREATE TRIGGER confirmation_resolution_tenant_guard BEFORE INSERT ON confirmation_resolution FOR EACH ROW EXECUTE FUNCTION confirmation_tenant_guard();
CREATE TRIGGER confirmation_spend_tenant_guard BEFORE INSERT ON confirmation_spend FOR EACH ROW EXECUTE FUNCTION confirmation_tenant_guard();

-- Only an ALLOW decision of the spend's own tenant can spend a confirmation (REQ-TRU-008). The trigger runs as the inserting role, which is Trust and holds SELECT on decision_record, so row
-- security applies to its read. Its search_path is pinned to the catalog with pg_temp LAST and the table is schema-qualified, so a login holding TEMPORARY cannot shadow decision_record with a
-- temporary table holding an ALLOW row (the rule of decision_grant_requires_allow, 0012).
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

-- The confirmation exists for this tenant (row security hides another tenant's), has not expired by THE DATABASE'S clock (never the caller's), and was given for exactly the action the spend's
-- binding names. The three failures are one answer, on purpose: the caller learns only that the confirmation is not valid for this action (a confirmation oracle would say which). The same
-- invoker rule and search_path as above.
CREATE FUNCTION confirmation_spend_requires_confirmation() RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE given public.confirmation_resolution%ROWTYPE;
BEGIN
  SELECT * INTO given FROM public.confirmation_resolution c WHERE c.tenant_id = NEW.tenant_id AND c.confirmation_event_id = NEW.confirmation_event_id;
  IF NOT FOUND OR statement_timestamp() > given.expires_at OR given.binding_hash IS DISTINCT FROM NEW.binding_hash THEN
    RAISE EXCEPTION 'confirmation_spend: the confirmation is not valid for this action'
      USING ERRCODE = 'integrity_constraint_violation', CONSTRAINT = 'confirmation_spend_confirmation_valid';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER confirmation_spend_requires_confirmation BEFORE INSERT ON confirmation_spend FOR EACH ROW EXECUTE FUNCTION confirmation_spend_requires_confirmation();

-- Reads and writes are scoped to the login's tenant by row-level security on the same mapping as decision_record (decision_login_tenant(), 0012). The tables' owner is subject to the policies
-- too (FORCE); a superuser bypasses them, and no runtime login is one.
ALTER TABLE confirmation_resolution ENABLE ROW LEVEL SECURITY;
CREATE POLICY confirmation_resolution_read ON confirmation_resolution FOR SELECT TO trust_rw USING (tenant_id = decision_login_tenant());
CREATE POLICY confirmation_resolution_write ON confirmation_resolution FOR INSERT TO trust_rw WITH CHECK (tenant_id = decision_login_tenant());
ALTER TABLE confirmation_resolution FORCE ROW LEVEL SECURITY;
ALTER TABLE confirmation_spend ENABLE ROW LEVEL SECURITY;
CREATE POLICY confirmation_spend_read ON confirmation_spend FOR SELECT TO trust_rw USING (tenant_id = decision_login_tenant());
CREATE POLICY confirmation_spend_write ON confirmation_spend FOR INSERT TO trust_rw WITH CHECK (tenant_id = decision_login_tenant());
ALTER TABLE confirmation_spend FORCE ROW LEVEL SECURITY;
