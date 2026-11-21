-- 0024: policy_version_key(text) is no longer executable by PUBLIC (TRU-08 follow-up; A165, noema#462). REQ-SEC-004; ruling A6.
-- Number 0024 by A165. Forward-only: 0015 is merged and never edited. No CREATE ROLE (TestNoMigrationAfter0001CreatesARole).
--
-- Migration 0015 created policy_version_key(text) and, unlike every other non-trigger function in the public schema, did not revoke its default EXECUTE from PUBLIC,
-- so every role could call it. It is IMMUTABLE and harmless, but a login that is meant to hold EXECUTE on no function (the evidence_verify exception, EL's condition 3 on
-- noema#282) would hold it. The CHECK policy_publication_key_is_the_version calls it, and a CHECK is evaluated by the inserting role, so the one role that inserts a
-- publication keeps EXECUTE: trust_rw. The owner (the role that applied 0015) keeps it as the owner.
REVOKE ALL ON FUNCTION policy_version_key(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION policy_version_key(text) TO trust_rw;
