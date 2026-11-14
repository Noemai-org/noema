-- Checks the database roles that init.sh sets up (W1 D14, D14a; PERD §38.3, REQ-SEC-004). Run as the superuser against the
-- `noema` database: psql -v ON_ERROR_STOP=1 -U postgres -d noema -f assert_roles.sql. Every failed check raises, naming itself.
DO $$
DECLARE
  owner_name text;
  bad text;
BEGIN
  SELECT pg_get_userbyid(datdba) INTO owner_name FROM pg_database WHERE datname = 'noema';
  IF owner_name <> 'noema_migrator' THEN
    RAISE EXCEPTION 'database noema is owned by %, expected noema_migrator', owner_name;
  END IF;

  -- The core's login owns nothing and can create nothing.
  IF has_database_privilege('noema_core', 'noema', 'CREATE') THEN
    RAISE EXCEPTION 'noema_core can CREATE on database noema';
  END IF;
  IF has_database_privilege('noema_core', 'noema', 'TEMPORARY') THEN
    RAISE EXCEPTION 'noema_core can create temporary tables in noema';
  END IF;
  IF NOT has_database_privilege('noema_core', 'noema', 'CONNECT') THEN
    RAISE EXCEPTION 'noema_core cannot CONNECT to noema';
  END IF;
  IF has_schema_privilege('noema_core', 'public', 'CREATE') THEN
    RAISE EXCEPTION 'noema_core can CREATE in schema public';
  END IF;
  SELECT string_agg(n.nspname, ', ') INTO bad FROM pg_namespace n
    WHERE n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema' AND has_schema_privilege('noema_core', n.oid, 'CREATE');
  IF bad IS NOT NULL THEN
    RAISE EXCEPTION 'noema_core can CREATE in schema(s) %', bad;
  END IF;
  SELECT string_agg(c.relname, ', ') INTO bad FROM pg_class c JOIN pg_roles r ON r.oid = c.relowner WHERE r.rolname = 'noema_core';
  IF bad IS NOT NULL THEN
    RAISE EXCEPTION 'noema_core owns objects: %', bad;
  END IF;

  -- No path from the core's login to the migrator, directly or through another role (recursive).
  IF EXISTS (
    WITH RECURSIVE reach(roleid) AS (
      SELECT oid FROM pg_roles WHERE rolname = 'noema_core'
      UNION
      SELECT m.roleid FROM pg_auth_members m JOIN reach ON m.member = reach.roleid)
    SELECT 1 FROM reach JOIN pg_roles r ON r.oid = reach.roleid WHERE r.rolname = 'noema_migrator') THEN
    RAISE EXCEPTION 'noema_core is a member of noema_migrator (directly or not)';
  END IF;

  -- The six power roles exist, cannot log in and are not members of anything that reaches the migrator.
  SELECT string_agg(r, ', ') INTO bad FROM unnest(ARRAY['trust_rw','execution_rw','outcome_rw','record_append','evidence_rw','orchestrator_rw']) r
    WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r AND NOT rolcanlogin);
  IF bad IS NOT NULL THEN
    RAISE EXCEPTION 'power role(s) missing or able to log in: %', bad;
  END IF;
  IF EXISTS (
    WITH RECURSIVE reach(roleid) AS (
      SELECT oid FROM pg_roles WHERE rolname IN ('trust_rw','execution_rw','outcome_rw','record_append','evidence_rw','orchestrator_rw')
      UNION
      SELECT m.roleid FROM pg_auth_members m JOIN reach ON m.member = reach.roleid)
    SELECT 1 FROM reach JOIN pg_roles r ON r.oid = reach.roleid WHERE r.rolname = 'noema_migrator') THEN
    RAISE EXCEPTION 'a power role is a member of noema_migrator (directly or not)';
  END IF;

  -- The migrator role cannot log in; only its one login can, and that login is not the core's.
  IF (SELECT rolcanlogin FROM pg_roles WHERE rolname = 'noema_migrator') THEN
    RAISE EXCEPTION 'noema_migrator can log in';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_auth_members m JOIN pg_roles g ON g.oid = m.roleid JOIN pg_roles u ON u.oid = m.member
                 WHERE g.rolname = 'noema_migrator' AND u.rolname = 'noema_migration_login' AND u.rolcanlogin) THEN
    RAISE EXCEPTION 'noema_migration_login is not a login member of noema_migrator';
  END IF;
  -- Neither is a superuser or can create roles or databases (the superuser stays the bootstrap's alone).
  SELECT string_agg(rolname, ', ') INTO bad FROM pg_roles
    WHERE rolname IN ('noema_core', 'noema_migrator', 'noema_migration_login') AND (rolsuper OR rolcreatedb OR rolreplication OR rolbypassrls);
  IF bad IS NOT NULL THEN
    RAISE EXCEPTION 'over-privileged role(s): %', bad;
  END IF;

  -- The evidence recompute's login (EVD-03, D9): it logs in, is a member of no role, holds no attribute, may connect, and can create nothing.
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'evidence_verify' AND rolcanlogin AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole
                 AND NOT rolreplication AND NOT rolbypassrls) THEN
    RAISE EXCEPTION 'evidence_verify is missing, cannot log in, or holds an attribute';
  END IF;
  IF EXISTS (SELECT 1 FROM pg_auth_members m JOIN pg_roles u ON u.oid = m.member WHERE u.rolname = 'evidence_verify') THEN
    RAISE EXCEPTION 'evidence_verify is a member of a role';
  END IF;
  IF NOT has_database_privilege('evidence_verify', 'noema', 'CONNECT') OR has_database_privilege('evidence_verify', 'noema', 'CREATE')
     OR has_database_privilege('evidence_verify', 'noema', 'TEMPORARY') OR has_schema_privilege('evidence_verify', 'public', 'CREATE') THEN
    RAISE EXCEPTION 'evidence_verify: CONNECT is missing, or it can CREATE or create temporary tables';
  END IF;
END $$;
SELECT 'roles ok';
