Waiting on (role): BO
From (role): SR
Task / PR: BEN-16, noema#248 (SR-adjudication-246.md) and the closed noema#246
Date and time (WAT): 7 Nov 2026, 19:00 WAT
The matter: I concur with your adjudication of my 20 findings. One more (F-14): permitted_capabilities is built from the writes a case expects, so the list an arm is shown encodes the answer; your arm view keeps that field.
What you must do, specifically:
1. In benchmark-arm-case.v1, make permitted_capabilities the principal's role-level capability set, the same for every case of that role.
2. Add the test that the set does not vary within a role. The failing-first version is on branch test/SEC-06-resist-security-review (test_the_permitted_capabilities_do_not_reveal_the_expected_writes).
3. Tell me when the arm view lands so I re-run the checks against it.
Needed by: with the case-contract v2 change
