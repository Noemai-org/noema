-- 0023: the dispatch budget is enforced by the database (RCV-02 slice 2; ruling A153 of DEC-2026-11-12-architect-rulings-a148-a160; REQ-RCV-003; INV-03, AGENT-08).
-- Forward-only: never edited after merge. The number 0023 is the one A153 names and the ledger of noema#183 (ruling A167) confirms; 0022 is reserved there for another migration, which does not block this one (A164): `Migrate` applies by name and accepts a gap.
--
-- A key is the tenant, the correlation, the capability and the target of a dispatch (ruling A3). The Record may hold at most two DISPATCH_ATTEMPTED events of a key
-- (A3: the budget is 2; A62: the pack may lower it and never raise it above 2, so 2 is the ceiling the database holds whatever a caller or a pack says). Spending a
-- grant is what leads to a dispatch, so the guard sits on the insert into grant_spend, which Spend makes in the same transaction as the GRANT_SPENT and
-- DISPATCH_ATTEMPTED events: before the spend is stored, the trigger reads the grant's key and counts the key's DISPATCH_ATTEMPTED events in the Record; at two it
-- refuses with NS002 (AFTER INSERT, so the table's own unique constraints on the grant and the nonce answer first and unchanged). Spend then rolls back as a whole, so nothing is spent, written or dispatched, and the same grant is refused again every time it is tried.
--
-- Why on grant_spend and not on the DISPATCH_ATTEMPTED insert: the Record accepts events of that type from many writers and tests with payloads that carry no key, and a
-- rule on the event would have refused all of them. What must be impossible is a third spend of a key, and that is what this refuses. Appending DISPATCH_ATTEMPTED
-- events directly cannot dispatch anything and only uses up the key's own budget.
--
-- Fail closed, as the Go gate does (execution/recovery): a grant whose capability, target or correlation cannot be read is refused, because it might belong to any key
-- of its correlation; and an earlier DISPATCH_ATTEMPTED event whose key cannot be read counts against every key of its correlation.
-- Concurrency: the rule does not depend on the isolation level of the spending transaction (Security Reviewer, noema#472). The guard takes a transaction-scoped advisory
-- lock on the key before it counts, so a second spend of the key waits for the first to commit (or roll back) and then counts the Record with the first's event in it:
-- under READ COMMITTED it sees two and is refused; under SERIALIZABLE (what Spend uses) it waits, and the serialisation check still refuses a commit on a stale count.
-- The lock is released at the end of the transaction; two different keys share a lock only by a hash collision, which costs a wait and nothing else.
--
-- Two functions. dispatch_budget_count is SECURITY DEFINER, owned by record_append like the other Record functions, and counts for the tenant of the calling login
-- (record_login_tenant, never an argument, DEBT-04), so a login learns only its own tenant's counts; only execution_rw may call it. The trigger function is not
-- definer: it reads the grant as the spender, which Execution may (decision_grant is readable by execution_rw for its tenant), and asks the first for the count.
-- A superuser is not held to the rule (it can switch any trigger off, and no service login is a superuser: kill_switch_test.go, init.sh). The search_path of the
-- definer is pinned with pg_temp last and every name is schema-qualified. ENABLE ALWAYS so that session_replication_role = replica does not switch the trigger
-- off for a session. The count has its own partial index.

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'noema_migrator') THEN
    RAISE EXCEPTION 'the role noema_migrator does not exist: create it in deploy/compose/postgres/init.sh before migrating';
  END IF;
END $$;

CREATE INDEX record_event_dispatch_key_idx ON public.record_event (tenant_id, correlation_id) WHERE event_type = 'DISPATCH_ATTEMPTED';

-- The number of DISPATCH_ATTEMPTED events in the Record that count against a key, for the tenant of the calling login: those of the key itself, and those whose key
-- cannot be read, which might belong to any key of the correlation.
CREATE FUNCTION public.dispatch_budget_count(p_correlation_id text, p_capability text, p_target_ref text) RETURNS integer
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
DECLARE v_tenant text := public.record_login_tenant('write'); v_count integer;
BEGIN
  SELECT pg_catalog.count(*) INTO v_count
    FROM public.record_event e
   WHERE e.tenant_id = v_tenant AND e.correlation_id = p_correlation_id AND e.event_type = 'DISPATCH_ATTEMPTED'
     AND (pg_catalog.jsonb_typeof(e.payload) IS DISTINCT FROM 'object'
          OR COALESCE(e.payload ->> 'capability', '') = '' OR COALESCE(e.payload ->> 'target_ref', '') = ''
          OR (e.payload ->> 'capability' = p_capability AND e.payload ->> 'target_ref' = p_target_ref));
  RETURN v_count;
END $$;
-- The new owner needs CREATE on the schema for ALTER ... OWNER TO when the migrator is not a superuser (PostgreSQL 15 and later); the grant is held for this statement only (as 0019 does).
GRANT CREATE ON SCHEMA public TO record_append;
ALTER FUNCTION public.dispatch_budget_count(text, text, text) OWNER TO record_append;
REVOKE CREATE ON SCHEMA public FROM record_append;
REVOKE ALL ON FUNCTION public.dispatch_budget_count(text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.dispatch_budget_count(text, text, text) TO execution_rw;

CREATE FUNCTION public.grant_spend_budget_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE
  c_budget CONSTANT integer := 2;
  v_capability text;
  v_target text;
  v_corr text;
BEGIN
  IF pg_catalog.current_setting('is_superuser') = 'on' THEN
    RETURN NEW;
  END IF;
  SELECT g.json ->> 'capability_id', g.json ->> 'target_ref', g.json ->> 'correlation_id' INTO v_capability, v_target, v_corr
    FROM public.decision_grant g WHERE g.grant_id = NEW.grant_id AND g.tenant_id = NEW.tenant_id;
  IF NOT FOUND OR COALESCE(v_capability, '') = '' OR COALESCE(v_target, '') = '' OR COALESCE(v_corr, '') = '' THEN
    RAISE EXCEPTION 'dispatch budget: the dispatch key of grant % cannot be read', NEW.grant_id USING ERRCODE = 'NS002';
  END IF;
  IF pg_catalog.current_setting('transaction_isolation') = 'repeatable read' THEN
    RAISE EXCEPTION 'dispatch budget: a spend under REPEATABLE READ keeps the snapshot of its first statement and could count a stale budget' USING ERRCODE = '0A000';
  END IF;
  PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended(NEW.tenant_id || pg_catalog.chr(31) || v_corr || pg_catalog.chr(31) || v_capability || pg_catalog.chr(31) || v_target, 0));
  IF public.dispatch_budget_count(v_corr, v_capability, v_target) >= c_budget THEN
    RAISE EXCEPTION 'dispatch budget spent: the key of grant % has already dispatched % times', NEW.grant_id, c_budget USING ERRCODE = 'NS002';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION public.grant_spend_budget_guard() FROM PUBLIC;

CREATE TRIGGER grant_spend_budget AFTER INSERT ON public.grant_spend
  FOR EACH ROW EXECUTE FUNCTION public.grant_spend_budget_guard();
ALTER TABLE public.grant_spend ENABLE ALWAYS TRIGGER grant_spend_budget;
