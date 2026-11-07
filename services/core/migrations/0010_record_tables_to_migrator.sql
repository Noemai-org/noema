-- 0010: the Record tables belong to the migration role, and record_append loses CREATE on public.
-- EXE-08 (W4) slice A; closes DEBT-05. DEC-2026-11-01-receipt-and-record-signing D14, D15, D16 and section 6.1; test 7.
-- Forward-only: this file is never edited after merge (AGENTS.md section 7).
--
-- Until now record_event and record_chain_head were owned by record_append, the role whose definer functions append to them, and
-- record_append held CREATE on public (deploy/compose/postgres/init.sh, because migration 0001 hands objects to it and PostgreSQL
-- requires the new owner to hold CREATE). So the owner of the Record could be acted on by any session that can act as record_append,
-- and a verification trigger on the table would not have been out of its reach.
--
-- After this migration the tables are owned by noema_migrator. record_append keeps what its functions need as grants (not ownership):
-- INSERT and SELECT on record_event, SELECT, INSERT and UPDATE on record_chain_head. It can no longer alter, drop, truncate or
-- disable a trigger on either table, and it has no CREATE on any schema.
--
-- A later migration that must hand a new function to record_append has to GRANT CREATE ON SCHEMA public TO record_append at its start
-- and REVOKE it again at its end, in the same transaction (the migrating role owns the schema's CREATE right through noema_migrator).

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'noema_migrator') THEN
    RAISE EXCEPTION 'the role noema_migrator does not exist: create it in deploy/compose/postgres/init.sh before migrating';
  END IF;
END $$;

ALTER TABLE record_event OWNER TO noema_migrator;
ALTER TABLE record_chain_head OWNER TO noema_migrator;

GRANT SELECT, INSERT ON record_event TO record_append;
GRANT SELECT, INSERT, UPDATE ON record_chain_head TO record_append;

REVOKE CREATE ON SCHEMA public FROM record_append;
