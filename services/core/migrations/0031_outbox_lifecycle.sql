-- 0031: the outbox lifecycle (PLT-08; REQ-VER-005; PERD §38.2, `outbox_event`: PENDING to DISPATCHED, and §56, "Outbox"; INV-12). Forward-only: never edited after merge.
-- No CREATE ROLE here (TestNoMigrationAfter0001CreatesARole). The number is proposed: the ledger of noema#183 had 0029 and 0030 assigned and "next free: 0031" on 17 Nov 2026;
-- the Repo Steward confirms it, and if merge order changes the later PR renumbers (A34). `Migrate` applies by name and accepts a gap.
--
-- The grants of 0001 (INSERT for the producers, UPDATE (dispatched_at) for the dispatcher) leave two holes in the lifecycle that PERD §38.2 gives the table:
--   1. a producer may insert a row that is already dispatched: an event that commits with its state change and is never delivered (a lost event);
--   2. the dispatcher may set dispatched_at back to NULL, or to another instant: a delivery nobody asked for, and a record of delivery that was rewritten.
-- This trigger makes the lifecycle one way, for every role including the owner and the superuser (a trigger fires for them; row security does not apply to a superuser):
--   - an event is created pending;
--   - the only change an event ever undergoes is its dispatched_at moving from NULL to a value, once, with the ID, tenant, topic, payload and creation time unchanged;
--   - a dispatched event never changes again.
-- DELETE is not touched: nobody holds it today, and the retention job (DATA-01, 30 days after dispatch, PERD §38.2) will need it.
--
-- The function reads no relation. It is pinned to pg_catalog, pg_temp (the rule of noema#234) and runs as the invoker.

CREATE FUNCTION outbox_event_lifecycle() RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.dispatched_at IS NOT NULL THEN
      RAISE EXCEPTION 'outbox_event: an event is created pending' USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    RETURN NEW;
  END IF;
  IF OLD.dispatched_at IS NOT NULL THEN
    RAISE EXCEPTION 'outbox_event: a dispatched event never changes' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  IF NEW.dispatched_at IS NULL
     OR (NEW.outbox_id, NEW.tenant_id, NEW.topic, NEW.payload, NEW.created_at) IS DISTINCT FROM (OLD.outbox_id, OLD.tenant_id, OLD.topic, OLD.payload, OLD.created_at) THEN
    RAISE EXCEPTION 'outbox_event: the only change of a pending event is its dispatch' USING ERRCODE = 'integrity_constraint_violation';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER outbox_event_lifecycle BEFORE INSERT OR UPDATE ON outbox_event FOR EACH ROW EXECUTE FUNCTION outbox_event_lifecycle();
