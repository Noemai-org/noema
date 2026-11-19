-- 0034: the single-decision key of an input tuple includes the confirmation state (TRU-07 follow-up; ruling A366 on noema#759, interim and relayed in a session, not yet committed; rulings A172
-- and A186 and migration 0027; PERD REQ-TRU-007, REQ-TRU-008, REQ-TRU-009; INV-21, INV-25). Number 0034 is the next free one after 0030 (noema#716), 0031 (outbox), 0032 (the confirmation
-- store, noema#756) and 0033 (admitted_intent.logical_resolution_epoch) that the migration ledger on noema#183 registers; it is chosen by C1 and to be confirmed there.
-- Forward-only: never edited after merge. No CREATE ROLE here (TestNoMigrationAfter0001CreatesARole).
--
-- Migration 0027 allowed ONE decision per (tenant, intent ID, intent hash, snapshot, policy version) whatever its outcome, so after a stored CONFIRM the ALLOW that follows an ACCEPT of its
-- request, or the REJECT that follows a DECLINE, on the SAME snapshot was a conflict. A decision may now rest on a confirmation: confirmation_request_id is the confirmation request (cfr_...)
-- the decision rests on, the request its spent confirmation resolved (an ALLOW that follows an ACCEPT) or the declined request (a REJECT DECLINED_BY_APPROVER), and NULL for a decision that
-- rests on none (a CONFIRM, and every other decision). The column is nullable on purpose: NULL is the absence of a confirmation, not an unknown value; existing rows keep it.
--
-- The new key is the tuple and COALESCE of the state, so a CONFIRM (NULL) and a later ALLOW that rests on an ACCEPTED confirmation (non-NULL) on one snapshot are two decisions, while a second
-- decision of the same state is still one: a repeated Record returns the stored decision (INV-21). COALESCE, not the bare column, because two NULLs are never equal in a unique index, which
-- would allow any number of decisions of the NULL state. The empty text cannot be a real state (the check below requires a cfr_ ID), so it never collides with one. tenant_id is first.
-- The old key is dropped after the new one exists. The immutability triggers of decision_record (0012) are not touched: a new column with no default rewrites no row and fires no UPDATE trigger.
-- If rows already hold a duplicate of the new key this migration fails and a person must look at the database; nothing is deleted (decision_record is insert-only).
ALTER TABLE decision_record ADD COLUMN confirmation_request_id text;
ALTER TABLE decision_record ADD CONSTRAINT decision_record_confirmation_request_form CHECK (confirmation_request_id IS NULL OR confirmation_request_id ~ '^cfr_[0-9A-Za-z]{2,32}$');
CREATE UNIQUE INDEX decision_record_input_state_key ON decision_record
  (tenant_id, (json ->> 'intent_id'), intent_hash, (json ->> 'evidence_snapshot_id'), (json ->> 'policy_version'), (COALESCE(confirmation_request_id, '')));
DROP INDEX decision_record_input_key;
