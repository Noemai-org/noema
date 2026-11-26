-- 0043: only an ESCALATE names a turn and a verified principal (TRU-10; ruling A709, interim, relayed and recorded in DEC-2026-11-26-architect-rulings-a704-a720; PERD REQ-TRU-010; INV-03,
-- INV-17, INV-21, INV-25). Forward-only: never edited after merge. No CREATE ROLE here (TestNoMigrationAfter0001CreatesARole).
--
-- Migration 0040 let any outcome that is not an ALLOW or a CONFIRM carry the pair (turn_id, principal_id). The early gate writes an ESCALATE and nothing else (A663, A574, A577: an
-- early decision is an ESCALATE whose basis is null), and the Writer already refuses a turn on any other decision (`only an early ESCALATE names a turn and a principal`). This migration
-- makes the database say the same: decision_record_early_key_shape is replaced by a CHECK of the same shape in which the outcome must be exactly 'ESCALATE'. A decision with neither
-- column is unchanged. Only the CHECK is replaced; no index changes, no row is written and no trigger fires. The new CHECK is validated against every existing row: if a row holds a turn
-- on another outcome (none can on a database written by this code) the migration fails and a person must look; nothing is deleted (decision_record is insert-only).

ALTER TABLE decision_record DROP CONSTRAINT decision_record_early_key_shape;

ALTER TABLE decision_record ADD CONSTRAINT decision_record_early_key_shape CHECK (
    (turn_id IS NULL AND principal_id IS NULL)
    OR coalesce(
         turn_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
         AND principal_id ~ '^usr_[0-9A-Za-z]{2,32}$'
         AND outcome = 'ESCALATE'
         AND jsonb_typeof(json -> 'evidence_snapshot_id') = 'null', false));
