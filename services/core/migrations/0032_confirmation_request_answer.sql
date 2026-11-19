-- 0032: the confirmation REQUEST and the approver's ANSWER (TRU-12; the store the confirmation path lacks; Founder decision of 19 Nov 2026 on the full confirmation path, interim, relayed, not yet
-- committed; PERD REQ-TRU-009, REQ-AUD-001, REQ-SEC-004; SoT 43.2a; INV-11, INV-12, INV-17, INV-25). Number 0032 is claimed for EL on the merge board (noema#183). Forward-only: never edited
-- after merge. No CREATE ROLE here (TestNoMigrationAfter0001CreatesARole). `Migrate` applies by name and accepts a gap (0030).
--
-- confirmation_request: one request for a confirmation, bound by the EIGHT values of SoT 43.2a (principal, intent hash, capability, target, parameters hash, evidence-set hash, plan hash, policy
-- version), all NOT NULL, in the form trust/confirm.BindingHash accepts, and the binding hash itself, which the database recomputes with the same canonical form and refuses on any difference. A
-- request is never updated: a fresh one SUPERSEDES the old one (supersedes_request_id), so the chain of requests of one action is the history. Its ID has the form of 0028 (cfr_...).
-- confirmation_answer: the approver's reply to one request (ACCEPT or DECLINE), with the approver's principal and roles as the authenticated session gave them, insert-only; one answer per
-- request (A341: the first answer wins). Its ID has the form of 0028 (cfe_...), so an ACCEPT that resolves to PROCEED_TO_FRESH_EVALUATION becomes the confirmation_resolution row of 0028 with the same event ID.
-- confirmation_expiry: the one-time mark that CONFIRMATION_EXPIRED was recorded for a request (a third table, needed so that the expiry is recorded once and never before the request's expiry).
--
-- One open request per (tenant, intent, capability, target): a root request (no supersedes) is unique on that key, and a request is superseded at most once, so the chain of an action is linear
-- and has one head. A fresh request may supersede its predecessor only when the predecessor has an answer (an ACCEPT whose binding or policy changed) or has expired by the new request's own
-- time: never a second request beside an open, unexpired, unanswered one. The only foreign key is to tenant; every other check is a trigger, and nothing cascades.
--
-- A368 (Founder order of 19 Nov 2026, interim, relayed, not yet committed; Codex, Security Reviewer and C2 reads of noema#756): (i) one answer per request, whoever the approver (UNIQUE below);
-- (ii) every time is the DATABASE clock: requested_at, answered_at, observed_at and recorded_at are set to now() by the BEFORE INSERT triggers whatever the caller supplies, and the expiry rules
-- compare against that clock inside the same statement, so no caller can expire a request early, answer it late or order two answers by a time of its own; (iii) intent_version >= 1; (iv) an answer after
-- the request expired (SQLSTATE 23E01) or after it was superseded (23S01) is refused by the answer trigger, which takes the same per-request advisory lock as the superseding insert, so a concurrent
-- supersede and answer are serialised and never leave an answer on a superseded request. A replay of a stored answer is not refused here: the unique key decides it.
-- Trust alone reads and writes these tables (trust_rw), bound to the tenant of its login by row security and by the tenant guard of 0028.

CREATE TABLE confirmation_request (
  tenant_id               text NOT NULL REFERENCES tenant (tenant_id),
  confirmation_request_id text NOT NULL CHECK (confirmation_request_id ~ '^cfr_[0-9A-Za-z]{2,32}$'),
  correlation_id          text NOT NULL CHECK (correlation_id ~ '^[A-Za-z0-9_.:@-]+$'),
  intent_id               text NOT NULL CHECK (intent_id ~ '^[A-Za-z0-9_.:@-]+$'),
  intent_version          integer NOT NULL CHECK (intent_version >= 1),
  -- the eight bound values (SoT 43.2a); the character set is the one trust/confirm.BindingHash needs, so the canonical form needs no escaping
  principal_id            text NOT NULL CHECK (principal_id ~ '^[A-Za-z0-9_.:@-]+$'),
  intent_hash             text NOT NULL CHECK (intent_hash ~ '^[A-Za-z0-9_.:@-]+$'),
  capability_id           text NOT NULL CHECK (capability_id ~ '^[A-Za-z0-9_.:@-]+$'),
  target_ref              text NOT NULL CHECK (target_ref ~ '^[A-Za-z0-9_.:@-]+$'),
  params_hash             text NOT NULL CHECK (params_hash ~ '^[A-Za-z0-9_.:@-]+$'),
  evidence_set_hash       text NOT NULL CHECK (evidence_set_hash ~ '^[A-Za-z0-9_.:@-]+$'),
  plan_hash               text NOT NULL CHECK (plan_hash ~ '^[A-Za-z0-9_.:@-]+$'),
  policy_version          text NOT NULL CHECK (policy_version ~ '^[A-Za-z0-9_.:@-]+$'),
  binding_hash            text NOT NULL CHECK (binding_hash ~ '^sha256:[0-9a-f]{64}$'),
  requirement             text NOT NULL CHECK (requirement ~ '^(SELF|MAKER_CHECKER|ROLE:[A-Za-z0-9_.-]+)$'),
  requester_id            text NOT NULL CHECK (requester_id ~ '^[A-Za-z0-9_.:@-]+$'),
  requested_at            timestamptz NOT NULL DEFAULT now(),  -- always the database's now(): set by confirmation_request_chain()
  expires_at              timestamptz NOT NULL,
  supersedes_request_id   text CHECK (supersedes_request_id ~ '^cfr_[0-9A-Za-z]{2,32}$'),
  recorded_at             timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT confirmation_request_pkey PRIMARY KEY (tenant_id, confirmation_request_id),
  CONSTRAINT confirmation_request_expiry_after_request CHECK (expires_at > requested_at),
  CONSTRAINT confirmation_request_not_self_superseding CHECK (supersedes_request_id IS DISTINCT FROM confirmation_request_id),
  -- the same canonical form as trust/confirm.BindingHash: sorted keys, no white space, string values
  CONSTRAINT confirmation_request_binding_hash_matches CHECK (binding_hash = 'sha256:' || encode(sha256(convert_to(
    '{"capability_id":"' || capability_id || '","evidence_set_hash":"' || evidence_set_hash || '","intent_hash":"' || intent_hash || '","params_hash":"' || params_hash
    || '","plan_hash":"' || plan_hash || '","policy_version":"' || policy_version || '","principal_id":"' || principal_id || '","target_ref":"' || target_ref || '"}', 'UTF8')), 'hex'))
);

-- one root request per action, and one successor per request: the chain of an action is linear, so there is one open request
CREATE UNIQUE INDEX confirmation_request_root_key ON confirmation_request (tenant_id, intent_id, capability_id, target_ref) WHERE supersedes_request_id IS NULL;
CREATE UNIQUE INDEX confirmation_request_successor_key ON confirmation_request (tenant_id, supersedes_request_id) WHERE supersedes_request_id IS NOT NULL;
-- the query path of the store: the newest request of an intent of a principal
CREATE INDEX confirmation_request_latest_idx ON confirmation_request (tenant_id, principal_id, intent_id, requested_at DESC, confirmation_request_id DESC);

CREATE TABLE confirmation_answer (
  tenant_id               text NOT NULL REFERENCES tenant (tenant_id),
  confirmation_event_id   text NOT NULL CHECK (confirmation_event_id ~ '^cfe_[0-9A-Za-z]{2,32}$'),
  confirmation_request_id text NOT NULL CHECK (confirmation_request_id ~ '^cfr_[0-9A-Za-z]{2,32}$'),
  response                text NOT NULL CHECK (response IN ('ACCEPT', 'DECLINE')),
  approver_id             text NOT NULL CHECK (approver_id ~ '^[A-Za-z0-9_.:@-]+$'),
  -- the roles the authenticated session gave the approver (INV-12); never from the request body
  approver_roles          text[] NOT NULL,
  answered_at             timestamptz NOT NULL DEFAULT now(),  -- always the database's now(): set by confirmation_answer_requires_request()
  recorded_at             timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT confirmation_answer_pkey PRIMARY KEY (tenant_id, confirmation_event_id),
  -- A341 (interim, relayed, not yet committed): the FIRST answer wins, so a request has at most one answer, whoever the approver; a second answer is refused here, not only in code
  CONSTRAINT confirmation_answer_request_key UNIQUE (tenant_id, confirmation_request_id)
);

CREATE TABLE confirmation_expiry (
  tenant_id               text NOT NULL REFERENCES tenant (tenant_id),
  confirmation_request_id text NOT NULL CHECK (confirmation_request_id ~ '^cfr_[0-9A-Za-z]{2,32}$'),
  observed_at             timestamptz NOT NULL DEFAULT now(),  -- always the database's now(): set by confirmation_expiry_requires_expired_request()
  recorded_at             timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT confirmation_expiry_pkey PRIMARY KEY (tenant_id, confirmation_request_id)
);

-- Insert-only, like the Record and the decisions: a row is never edited, deleted or truncated, by any role.
CREATE TRIGGER confirmation_request_immutable BEFORE UPDATE OR DELETE ON confirmation_request FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER confirmation_request_no_truncate BEFORE TRUNCATE ON confirmation_request FOR EACH STATEMENT EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER confirmation_answer_immutable BEFORE UPDATE OR DELETE ON confirmation_answer FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER confirmation_answer_no_truncate BEFORE TRUNCATE ON confirmation_answer FOR EACH STATEMENT EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER confirmation_expiry_immutable BEFORE UPDATE OR DELETE ON confirmation_expiry FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER confirmation_expiry_no_truncate BEFORE TRUNCATE ON confirmation_expiry FOR EACH STATEMENT EXECUTE FUNCTION reject_mutation();

-- A write is bound to the tenant of the login that makes it: confirmation_tenant_guard() of 0028 (session_user, tenant_login, power 'trust').
CREATE TRIGGER confirmation_request_tenant_guard BEFORE INSERT ON confirmation_request FOR EACH ROW EXECUTE FUNCTION confirmation_tenant_guard();
CREATE TRIGGER confirmation_answer_tenant_guard BEFORE INSERT ON confirmation_answer FOR EACH ROW EXECUTE FUNCTION confirmation_tenant_guard();
CREATE TRIGGER confirmation_expiry_tenant_guard BEFORE INSERT ON confirmation_expiry FOR EACH ROW EXECUTE FUNCTION confirmation_tenant_guard();

REVOKE ALL ON confirmation_request FROM PUBLIC;
REVOKE ALL ON confirmation_answer FROM PUBLIC;
REVOKE ALL ON confirmation_expiry FROM PUBLIC;
GRANT SELECT, INSERT ON confirmation_request TO trust_rw;   -- only Trust asks for a confirmation
GRANT SELECT, INSERT ON confirmation_answer TO trust_rw;    -- and records the reply the route authenticated
GRANT SELECT, INSERT ON confirmation_expiry TO trust_rw;

-- The chain of one action. The functions run as the inserting role (Trust, which holds SELECT, so row security applies to their reads), with the search_path pinned to the catalog and pg_temp
-- LAST and every table schema-qualified (the rule of 0012 and 0028).
CREATE FUNCTION confirmation_request_chain() RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE prior public.confirmation_request%ROWTYPE;
BEGIN
  NEW.requested_at := now();   -- A368 (ii): the database's clock, never the caller's
  NEW.recorded_at := now();
  IF NEW.supersedes_request_id IS NULL THEN
    IF EXISTS (SELECT 1 FROM public.confirmation_request r WHERE r.tenant_id = NEW.tenant_id AND r.intent_id = NEW.intent_id
               AND r.capability_id = NEW.capability_id AND r.target_ref = NEW.target_ref) THEN
      RAISE EXCEPTION 'confirmation_request: the action already has a request; a fresh request must supersede it' USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    RETURN NEW;
  END IF;
  -- serialised with an answer to the same request (A368 (iv)): both take this lock before they read, and it is held to the end of the transaction
  PERFORM pg_advisory_xact_lock(hashtextextended('confirmation_request:' || NEW.tenant_id || ':' || NEW.supersedes_request_id, 0));
  SELECT * INTO prior FROM public.confirmation_request r WHERE r.tenant_id = NEW.tenant_id AND r.confirmation_request_id = NEW.supersedes_request_id;
  IF NOT FOUND OR (prior.principal_id, prior.intent_id, prior.capability_id, prior.target_ref)
                  IS DISTINCT FROM (NEW.principal_id, NEW.intent_id, NEW.capability_id, NEW.target_ref) THEN
    RAISE EXCEPTION 'confirmation_request: a request supersedes only a request of its own tenant, principal and action' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  -- never a second request beside an open one: the predecessor was answered, or it has expired by the new request's own time
  IF prior.expires_at > NEW.requested_at AND NOT EXISTS (SELECT 1 FROM public.confirmation_answer a
        WHERE a.tenant_id = prior.tenant_id AND a.confirmation_request_id = prior.confirmation_request_id) THEN
    RAISE EXCEPTION 'confirmation_request: the open request is neither answered nor expired' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER confirmation_request_chain BEFORE INSERT ON confirmation_request FOR EACH ROW EXECUTE FUNCTION confirmation_request_chain();

-- An answer names a request of its own tenant (row security hides another tenant's) that is live on the database clock: not expired (23E01) and not superseded (23S01; decided first). A
-- request that already has an answer is left to the unique key (the first answer wins; a replay is the store's to recognise), so this trigger never turns a duplicate into an expiry.
CREATE FUNCTION confirmation_answer_requires_request() RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE req public.confirmation_request%ROWTYPE;
BEGIN
  NEW.answered_at := now();   -- A368 (ii): the database's clock, never the caller's
  NEW.recorded_at := now();
  PERFORM pg_advisory_xact_lock(hashtextextended('confirmation_request:' || NEW.tenant_id || ':' || NEW.confirmation_request_id, 0));
  SELECT * INTO req FROM public.confirmation_request r WHERE r.tenant_id = NEW.tenant_id AND r.confirmation_request_id = NEW.confirmation_request_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION '%: the request does not exist for this tenant', TG_TABLE_NAME USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  IF EXISTS (SELECT 1 FROM public.confirmation_answer a WHERE a.tenant_id = NEW.tenant_id AND a.confirmation_request_id = NEW.confirmation_request_id) THEN
    RETURN NEW;
  END IF;
  IF EXISTS (SELECT 1 FROM public.confirmation_request s WHERE s.tenant_id = NEW.tenant_id AND s.supersedes_request_id = NEW.confirmation_request_id) THEN
    RAISE EXCEPTION 'confirmation_answer: the request was superseded' USING ERRCODE = '23S01';
  END IF;
  IF req.expires_at <= NEW.answered_at THEN
    RAISE EXCEPTION 'confirmation_answer: the request has expired' USING ERRCODE = '23E01';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER confirmation_answer_requires_request BEFORE INSERT ON confirmation_answer FOR EACH ROW EXECUTE FUNCTION confirmation_answer_requires_request();

CREATE FUNCTION confirmation_expiry_requires_expired_request() RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  NEW.observed_at := now();   -- A368 (ii): the database's clock, so a caller cannot expire a request early by claiming a later time
  NEW.recorded_at := now();
  IF NOT EXISTS (SELECT 1 FROM public.confirmation_request r WHERE r.tenant_id = NEW.tenant_id AND r.confirmation_request_id = NEW.confirmation_request_id
                 AND r.expires_at <= NEW.observed_at) THEN
    RAISE EXCEPTION 'confirmation_expiry: the request does not exist for this tenant or has not expired' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER confirmation_expiry_requires_expired_request BEFORE INSERT ON confirmation_expiry FOR EACH ROW EXECUTE FUNCTION confirmation_expiry_requires_expired_request();

-- Reads and writes are scoped to the login's tenant by row-level security on the mapping of 0012 (decision_login_tenant()), FORCE as in 0018: the owner is subject too; a superuser bypasses
-- row security, and no runtime login is one.
ALTER TABLE confirmation_request ENABLE ROW LEVEL SECURITY;
CREATE POLICY confirmation_request_read ON confirmation_request FOR SELECT TO trust_rw USING (tenant_id = decision_login_tenant());
CREATE POLICY confirmation_request_write ON confirmation_request FOR INSERT TO trust_rw WITH CHECK (tenant_id = decision_login_tenant());
ALTER TABLE confirmation_request FORCE ROW LEVEL SECURITY;
ALTER TABLE confirmation_answer ENABLE ROW LEVEL SECURITY;
CREATE POLICY confirmation_answer_read ON confirmation_answer FOR SELECT TO trust_rw USING (tenant_id = decision_login_tenant());
CREATE POLICY confirmation_answer_write ON confirmation_answer FOR INSERT TO trust_rw WITH CHECK (tenant_id = decision_login_tenant());
ALTER TABLE confirmation_answer FORCE ROW LEVEL SECURITY;
ALTER TABLE confirmation_expiry ENABLE ROW LEVEL SECURITY;
CREATE POLICY confirmation_expiry_read ON confirmation_expiry FOR SELECT TO trust_rw USING (tenant_id = decision_login_tenant());
CREATE POLICY confirmation_expiry_write ON confirmation_expiry FOR INSERT TO trust_rw WITH CHECK (tenant_id = decision_login_tenant());
ALTER TABLE confirmation_expiry FORCE ROW LEVEL SECURITY;
