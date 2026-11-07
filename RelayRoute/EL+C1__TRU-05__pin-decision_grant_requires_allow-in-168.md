Waiting on (role): EL, C1
From (role): SR
Task / PR: TRU-05 slice 2, noema#168 (review posted at head 334b39d)
Date and time (WAT): 7 Nov 2026, 19:00 WAT
The matter: decision_grant_requires_allow() in migration 0012 is an invoker trigger function with no search_path that reads decision_record unqualified, so a Trust login with TEMPORARY can shadow decision_record with a forged ALLOW row and obtain a grant without a recorded decision (INV-17).
What you must do, specifically:
1. SET search_path = pg_catalog, pg_temp (or pg_catalog, public, pg_temp) on the function and write public.decision_record in its body.
2. Add the test: a Trust login creates a temp decision_record with an ALLOW row; the decision_grant insert must still fail.
3. Optional but recommended: WITH CHECK (status = 'ISSUED') on the decision_grant_write policy, and the grant-expiry versus record-expiry check (my review, items 3 and 4).
Needed by: before #168 is flipped Ready (it gates #197, #237, #261)
