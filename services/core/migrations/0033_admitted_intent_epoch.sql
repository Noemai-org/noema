-- 0033: the logical resolution epoch of an admitted intent (ruling A345 S1 of the Architect, 19 Nov 2026; owner C2, because A151 gave C2 the admitted-intent store; REQ-INT-001, REQ-SEC-004;
-- INV-11, INV-17, INV-19; A151). Registered on C1's migration table (noema#183): 0030 is noema#716 and 0032 is noema#756, both open; Migrate has no gap check (it applies every
-- unapplied file in name order) and this file depends on 0026 alone, so it applies on a database with or without them.
-- Forward-only: this file is never edited after merge. No CREATE ROLE here (TestNoMigrationAfter0001CreatesARole).
--
-- What it holds: `logical_resolution_epoch`, an integer that is 1 at creation (A345 S1: "It is 1 at creation. The increment path is later debt."). Trust reads it as a fact of the intent
-- (the evidence of an earlier epoch is not the evidence of this one). NOT NULL, with DEFAULT 1 so that the rows already stored (created before the column existed) take 1 and a writer
-- that does not name the column gets 1; adding a column with a constant default is not an UPDATE, so the immutability trigger of 0026 (admitted_intent_immutable) never fires for it.
--
-- What it does not build: the increment path. The CHECK pins the value to 1, so no login (the orchestrator's included) can store another epoch before that path is designed; the
-- migration that builds the path replaces this constraint. The table stays insert-only: the existing triggers refuse every UPDATE, of this column too, for every role, and no role is
-- granted UPDATE on the table or the column. No new index: the column is read with the row, by the keys and the index that 0026 already names. Grants and row-level security are
-- those of the table; a new column needs none.

ALTER TABLE admitted_intent ADD COLUMN logical_resolution_epoch integer NOT NULL DEFAULT 1;
ALTER TABLE admitted_intent ADD CONSTRAINT admitted_intent_epoch_is_one CHECK (logical_resolution_epoch = 1);
