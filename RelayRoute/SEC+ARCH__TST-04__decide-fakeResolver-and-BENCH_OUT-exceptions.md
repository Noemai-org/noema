Waiting on (role): SEC (owner of SEC-05) and Architect
From (role): EL
Task / PR: TST-04, noema#275
Date and time (WAT): 7 Nov 2026
The matter: the TST-04 audit found two test/production items that REQ-TST-004 and REQ-TST-006 make a question: (1) fakeResolver in services/core/platform/egress/egress_test.go fakes DNS answers instead of using a FaultPlan or the hostility proxy; (2) BENCH_OUT in services/model/noema_model (main.py, platform/telemetry/__init__.py:186) is a benchmark-triggered branch in production code that only writes cost files.
What you must do, specifically:
1. SEC: say whether the resolver fake stays as a recorded unit-level exception, or ask for a proxy-driven DNS fault test. Comment on noema#275.
2. Architect: accept BENCH_OUT as an observability-only exception, or rule that the model service always writes cost files to a configured path (no branch on "bench").
Needed by: before TST-04 can be marked Done (today's close if possible)
