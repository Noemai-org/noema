-- 0021: operational memory (MEM-01; Founder decision D10 of 7 Nov 2026; REQ-MEM-001, REQ-MEM-002, INV-12, REQ-SEC-004). Forward-only: never edited after merge.
-- The number 0021 is the last of the noema#183 ledger (0011 #181, 0012 #168, 0013 #229, 0014 #233, 0015 #259, 0016 #234, 0017 #265 merged, 0018 #261, 0019 #263, 0020 #268, 0021 this PR);
-- the files merge in strict ascending order (A113), and the gap 0011 to 0016 is accepted.
--
-- Operational memory is data, never authority (REQ-MEM-001). The table is closed to every runtime role: no login of any power can read, write, update or delete
-- it. The one write path is memory_observation_record, a SECURITY DEFINER function that only the orchestrator's power (orchestrator_rw) may call and that takes
-- the tenant from the connecting login, not from an argument (the same binding as the Record functions, DEBT-04). Trust, Execution, Outcome and Evidence have no
-- privilege of any kind on it, so no decision path can read memory at decision time even by mistake. Reading by the ops inspector and the regression tooling
-- is a later slice with its own role; nothing here grants a read.
--
-- Row-level security is enabled and FORCED (ruling A6, REQ-SEC-004), so the table's owner, which is also the definer of memory_observation_record, is held to a policy too:
-- the one policy is an INSERT policy for the owner whose check is the connecting login's tenant (record_login_tenant('write')), so even the function cannot write
-- another tenant's row. There is no policy for SELECT, UPDATE, DELETE or ALL, so nobody but a superuser (which bypasses row security whatever is set; no runtime login
-- is one) can read or change a row through the table.
--
-- Rows are append-only (the immutability trigger of migration 0001 and a no-truncate trigger). An observation holds typed scalars only (REQ-MEM-001: no free
-- text): at most 16 members, each key lower-case snake_case, each value a non-negative integer, a boolean, an UPPER_SNAKE code or a dotted lower-case
-- identifier. The same shape as contracts/schemas/memory-observation.v2.json, enforced here as well as in Go (defence in depth).

CREATE FUNCTION public.memory_observation_is_typed(o jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE SET search_path = pg_catalog, pg_temp AS $$
  SELECT pg_catalog.jsonb_typeof(o) = 'object'
     AND (SELECT pg_catalog.count(*) FROM pg_catalog.jsonb_object_keys(o)) BETWEEN 1 AND 16
     AND NOT EXISTS (
       SELECT 1 FROM pg_catalog.jsonb_each(o) e
       WHERE e.key !~ '^[a-z][a-z0-9_]*$'
          OR NOT (
               (pg_catalog.jsonb_typeof(e.value) = 'number' AND e.value::text ~ '^[0-9]{1,15}$')
            OR pg_catalog.jsonb_typeof(e.value) = 'boolean'
            OR (pg_catalog.jsonb_typeof(e.value) = 'string' AND ((e.value #>> '{}') ~ '^[A-Z][A-Z0-9_]*$' OR (e.value #>> '{}') ~ '^[a-z][a-z0-9_]*(\.[a-z0-9_]+)+$'))
          ))
$$;
ALTER FUNCTION public.memory_observation_is_typed(jsonb) OWNER TO noema_migrator;
-- Not executable by PUBLIC (the same defect 0015 had, noema#462): a function created without a REVOKE is callable by every role. The CHECK below runs it as the owner, inside the
-- definer write function, which needs no grant.
REVOKE ALL ON FUNCTION public.memory_observation_is_typed(jsonb) FROM PUBLIC;

CREATE TABLE public.memory_observation (
  memory_id             text PRIMARY KEY CHECK (memory_id ~ '^mem_[A-Za-z0-9]+$'),
  tenant_id             text NOT NULL REFERENCES public.tenant (tenant_id),
  kind                  text NOT NULL CHECK (kind IN ('ADAPTER_BEHAVIOUR', 'FAILURE_PATTERN', 'RECOVERY_PATH', 'VERIFICATION_LATENCY', 'AMBIGUITY_PATTERN', 'REGRESSION_LINK')),
  subject_ref           text NOT NULL CHECK (subject_ref ~ '^[A-Za-z0-9_.:-]{1,128}$'),
  observation           jsonb NOT NULL,
  source_correlation_id text NOT NULL CHECK (source_correlation_id ~ '^corr_[A-Za-z0-9]+$'),
  observed_at           timestamptz NOT NULL,
  recorded_by           text NOT NULL CHECK (recorded_by ~ '^[A-Z][A-Z0-9_]*$'),
  provenance_ref        text NOT NULL CHECK (provenance_ref ~ '^[A-Z][A-Z0-9]*(-[A-Z0-9]+)+$'),
  recorded_at           timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT memory_observation_typed_scalars CHECK (public.memory_observation_is_typed(observation))
);
ALTER TABLE public.memory_observation OWNER TO noema_migrator;
REVOKE ALL ON public.memory_observation FROM PUBLIC;
CREATE INDEX memory_observation_tenant_kind_idx ON public.memory_observation (tenant_id, kind, recorded_at);
CREATE TRIGGER memory_observation_immutable BEFORE UPDATE OR DELETE ON public.memory_observation FOR EACH ROW EXECUTE FUNCTION public.reject_mutation();
CREATE TRIGGER memory_observation_no_truncate BEFORE TRUNCATE ON public.memory_observation FOR EACH STATEMENT EXECUTE FUNCTION public.reject_mutation();
ALTER TABLE public.memory_observation ENABLE ALWAYS TRIGGER memory_observation_immutable;
ALTER TABLE public.memory_observation ENABLE ALWAYS TRIGGER memory_observation_no_truncate;

-- The one write path. The tenant is the connecting login's (record_login_tenant also refuses a login with no mapping). EXECUTE is granted to orchestrator_rw only.
CREATE FUNCTION public.memory_observation_record(p_memory_id text, p_kind text, p_subject_ref text, p_observation jsonb, p_source_correlation_id text,
  p_observed_at timestamptz, p_recorded_by text, p_provenance_ref text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
DECLARE v_tenant text;
BEGIN
  IF NOT pg_catalog.pg_has_role(session_user, 'orchestrator_rw', 'MEMBER')
     OR pg_catalog.pg_has_role(session_user, 'trust_rw', 'MEMBER') OR pg_catalog.pg_has_role(session_user, 'execution_rw', 'MEMBER')
     OR pg_catalog.pg_has_role(session_user, 'outcome_rw', 'MEMBER') OR pg_catalog.pg_has_role(session_user, 'evidence_rw', 'MEMBER') THEN
    RAISE EXCEPTION 'memory_observation_record: the calling login is not the orchestrator''s own' USING ERRCODE = 'insufficient_privilege';
  END IF;
  v_tenant := public.record_login_tenant('write');
  INSERT INTO public.memory_observation (memory_id, tenant_id, kind, subject_ref, observation, source_correlation_id, observed_at, recorded_by, provenance_ref)
  VALUES (p_memory_id, v_tenant, p_kind, p_subject_ref, p_observation, p_source_correlation_id, p_observed_at, p_recorded_by, p_provenance_ref);
END $$;
ALTER FUNCTION public.memory_observation_record(text, text, text, jsonb, text, timestamptz, text, text) OWNER TO noema_migrator;
REVOKE ALL ON FUNCTION public.memory_observation_record(text, text, text, jsonb, text, timestamptz, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.memory_observation_record(text, text, text, jsonb, text, timestamptz, text, text) TO orchestrator_rw;
GRANT EXECUTE ON FUNCTION public.record_login_tenant(text) TO noema_migrator;

-- Row-level security, forced (ruling A6, REQ-SEC-004): see the header. The policy's expression runs as the owner, which holds EXECUTE on record_login_tenant above.
ALTER TABLE public.memory_observation ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.memory_observation FORCE ROW LEVEL SECURITY;
CREATE POLICY memory_observation_insert_own_tenant ON public.memory_observation FOR INSERT TO noema_migrator
  WITH CHECK (tenant_id = public.record_login_tenant('write'));
