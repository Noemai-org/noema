-- 0037: the increment path of the logical resolution epoch (DEBT-54 part 2; ruling A520 of the Architect, 22 Nov 2026, interim: "the subject is a pack-declared subject_slot on
-- the intent node, and the epoch comes from asking the Evidence reader for the subject's case list at admission and passing it into Save. No new table. It is migration 0037";
-- A513; A482: the epoch lives in Postgres on the row it governs, in the same transaction as the Record event; owner C2, because A151 gave C2 the admitted-intent store;
-- REQ-INT-001, REQ-SEC-004, REQ-AUD-001; INV-11, INV-17, INV-19). Depends on 0033 and 0026 alone.
-- Forward-only: this file is never edited after merge. No CREATE ROLE here (TestNoMigrationAfter0001CreatesARole).
--
-- What it changes. 0033 pinned `logical_resolution_epoch` to 1 ("the migration that builds the path replaces this constraint"): this is that migration. The CHECK (= 1) goes; the
-- column may now hold any positive integer. `subject_ref` is new: the resolved reference of the Resource slot the pack names `subject_slot` (part 1, the intent node's
-- optional member), in the shape of common.v1 ResourceRef (`<vertical>.<kind>:<id>`), NULL for an intent whose subject is not resolved or whose node declares none. An epoch above 1
-- needs a subject: an intent with no subject has epoch 1 (the safer reading of part 1, A520). The value is derived by the Interpreter from the subject's case list (PERD 28.2: the
-- epoch increments only when the prior logical action's case is verified CLOSED in the case system and a new intent is admitted [REC]); the store only keeps it.
--
-- What it does not change. The table stays insert-only: the triggers of 0026 refuse every UPDATE and DELETE for every role, of these columns too, and no role is granted UPDATE on
-- the table or on either column. No new table, no new role, no new grant: a new column needs none (INSERT and SELECT are the table's, and so is the row-level security).
-- No new index: neither column is a query path yet (the readers read by the keys and the index that 0026 already names). Rows stored before this migration keep epoch 1 and get no
-- subject: adding a nullable column with no default is not an UPDATE, so the immutability trigger never fires for it.

ALTER TABLE admitted_intent ADD COLUMN subject_ref text;
ALTER TABLE admitted_intent DROP CONSTRAINT admitted_intent_epoch_is_one;
ALTER TABLE admitted_intent ADD CONSTRAINT admitted_intent_epoch_positive CHECK (logical_resolution_epoch >= 1);
ALTER TABLE admitted_intent ADD CONSTRAINT admitted_intent_subject_ref_shape CHECK (subject_ref IS NULL OR subject_ref ~ '^[a-z][a-z0-9]*\.[a-z][a-z0-9_]*:[A-Za-z0-9_.-]{1,64}$');
ALTER TABLE admitted_intent ADD CONSTRAINT admitted_intent_epoch_needs_subject CHECK (subject_ref IS NOT NULL OR logical_resolution_epoch = 1);
