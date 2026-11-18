-- 0029: the investigation names the principal that started it (STM-01; A243, A273, A280; REQ-STM-001, REQ-SEC-004; INV-12).
-- Number 0029: the next free after 0028 on the ledger of noema#183 (0030, the tenant row-level security of A280, follows).
-- Forward-only: never edited after merge. No CREATE ROLE (TestNoMigrationAfter0001CreatesARole).
--
-- A route that serves an investigation must refuse another principal's request for it, and the row alone could not say who started it. The principal comes from the
-- authenticated session at Start (INV-12), never from the request. A row written before this migration has no principal to name: it is filled with the empty string and
-- kept out of the CHECK (NOT VALID), so no past row is rewritten or invented, and every new row must name one. The column is not in the orchestrator's column-limited
-- UPDATE grant of 0025, so it never changes after the insert.

ALTER TABLE investigation ADD COLUMN principal_ref text NOT NULL DEFAULT '';
ALTER TABLE investigation ALTER COLUMN principal_ref DROP DEFAULT;
ALTER TABLE investigation ADD CONSTRAINT investigation_principal_named CHECK (principal_ref <> '') NOT VALID;
