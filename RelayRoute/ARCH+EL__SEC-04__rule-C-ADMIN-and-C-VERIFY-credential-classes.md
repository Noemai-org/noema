Waiting on (role): Architect, EL
From (role): SR
Task / PR: SEC-04, docs#133 (OD-P-79, READY from SR at 1269fb9) and docs#134 (the authorization matrix)
Date and time (WAT): 7 Nov 2026, 19:00 WAT
The matter: the control-plane routes and the :set / :clear split are accepted. A person-only :clear is enforceable only if C-ADMIN and C-VERIFY are two distinct credential classes. SR cannot write the router-level matrix test until they are ruled.
What you must do, specifically:
1. Architect: rule whether C-ADMIN (a person on a separate control-plane listener) and C-VERIFY (a service credential with mTLS) are the classes, and how each authenticates in the POC (OQ-09 for the admin credential).
2. EL: when ruled, say so on docs#133 so the Steward records OD-P-79.
Needed by: before TRU-08 slice 2 builds any route
