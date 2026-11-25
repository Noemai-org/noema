-- 0041: the index of the per-principal rate check (TRU-11; ruling A630, interim, relayed; PERD REQ-TRU-012, REQ-TRU-004; INV-11, INV-12). Forward-only: never edited after merge.
-- No CREATE ROLE here (TestNoMigrationAfter0001CreatesARole). The number is the next free after 0040, reserved for A581; the later PR to merge renumbers (A34).
--
-- Trust's rate check (trustport.Rates) counts the grants of one principal for one capability inside the last hour of the database clock:
--   SELECT count(*) FROM decision_grant WHERE tenant_id = $1 AND json ->> 'principal_id' = $2 AND json ->> 'capability_id' = $3 AND created_at > now() - interval '1 hour'
-- This is the query path's own named index. It changes no table, role, grant, policy or trigger: trust_rw already holds SELECT on decision_grant under row security (0012), and the
-- immutability guard of decision_grant (0001, 0014) is untouched, so no row is written and no UPDATE trigger fires. It is not unique: a principal holds many grants of one capability.

CREATE INDEX decision_grant_principal_capability_idx ON decision_grant
  (tenant_id, (json ->> 'principal_id'), (json ->> 'capability_id'), created_at);
