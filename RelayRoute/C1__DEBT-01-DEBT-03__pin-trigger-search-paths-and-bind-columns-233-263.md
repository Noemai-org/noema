Waiting on (role): C1
From (role): SR
Task / PR: EXE-08 DEBT-01 and DEBT-03; noema#233, noema#263, noema#236 (reviews posted on each)
Date and time (WAT): 7 Nov 2026, 19:00 WAT
The matter: three fixes the closure evidence needs. (1) decision_grant_guard in #233 is neither SECURITY DEFINER nor pinned and reads execution_receipt and grant_spend unqualified, so a TEMPORARY table can shadow them. (2) record_event_verify in #263 binds seq, tenant_id, event_id, prev_hash and written_by but not correlation_id, actor, event_type, object_ref, payload_hash, at, schema_version, which the Record serves as columns. (3) The REC-08 closure test (forgery_test.go from #236) passes through SQLSTATE 42883 once the 14-argument record_insert_t is dropped.
What you must do, specifically:
1. #233: SET search_path = pg_catalog, pg_temp on decision_grant_guard and write public.execution_receipt and public.grant_spend; add the test described in my comment (a temp execution_receipt satisfying conditions a to f must not void the grant). My catalog check noema#251 (TestEveryTriggerFunctionPinsItsSearchPathToCatalogAndTemp) goes green when it is done.
2. #263: compare the seven columns to the signed document in the trigger and add one NS001 test per column.
3. #263: replace the err == nil assertion in forgery_test.go with an NS001 assertion on the 17-argument form, a separate 42883 case for the old form, and a check that no row is stored and the head is unchanged.
4. Post the new head SHA on each PR and reply here with an Answer section.
Needed by: before the final review of #233 and #263 (today if possible)
