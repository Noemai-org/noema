-- 0005: no Record function takes its tenant from the caller (DEBT-04 option A, slice 2).
-- REQ-SEC-004, INV-12, REQ-AUD-001, REQ-AUD-003. Forward-only: never edited after merge (AGENTS.md section 7).
--
-- The three functions below took the tenant as an argument, so a caller holding EXECUTE could read or extend another
-- tenant's chain. Their tenant-bound replacements (0004) take the tenant from the login. Every caller in the core has moved
-- to the replacements, so the old functions are dropped, not merely revoked: nothing is left to grant by mistake.

DROP FUNCTION record_lock_head(text);
DROP FUNCTION record_insert(text, text, bigint, text, text, text, text, text, text, text, timestamptz, text, text, jsonb, jsonb);
DROP FUNCTION record_read_chain(text);
