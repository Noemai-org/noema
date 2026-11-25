-- 0039: the investigation names the verified issuer of the session that started it (STM-07, PR A of A592; A584, interim, relayed, not yet committed). REQ-STM-001, REQ-SEC-004; INV-12, INV-30.
-- Number 0039: the number the ledger of noema#183 reserves for A584 (0037 is DEBT-54, 0038 is A574). If merge order changes, the later PR renumbers (ruling A34). Forward-only: 0036 is merged and never edited. No CREATE ROLE.
--
-- A session rebuilt from the stored row (recovery, noema#882) must say who verified it. The issuer comes from the verified session at creation (sess.AssertedBy, filled by the edge's session
-- verification), never from a request or a model. The column is NULLABLE by ruling A584: a row from before this migration has no issuer and none is invented for it (no default, no backfill).
-- Whether recovery refuses a row without it is PR B of A592, after this one merges; nothing here makes it refuse.
--
-- Form: the contract's asserted_by pattern (intent-object.v2 principal.asserted_by, ruling A163), the same as understanding.Session.Valid: ^[a-z][a-z0-9_-]*$. Postgres' $ matches at the end of the string only.
-- Unchanged: tenant_id, row-level security with FORCE, the per-power roles and the grants of 0025. The column is not in the orchestrator's column-limited UPDATE grant, so it never changes after the insert.
-- No index: no query path selects by it; the reads are by (tenant_id, investigation_id), the primary key.

ALTER TABLE investigation ADD COLUMN asserted_by text NULL;

ALTER TABLE investigation ADD CONSTRAINT investigation_asserted_by_form CHECK (asserted_by IS NULL OR asserted_by ~ '^[a-z][a-z0-9_-]*$');
