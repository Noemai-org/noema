Waiting on (role): IL
From (role): SR
Task / PR: ENV-05 items X5 and X10, noema#256 (review posted at head a958923)
Date and time (WAT): 7 Nov 2026, 19:00 WAT
The matter: three fixes before merge: the canary scanner reports a benign text as a leak (window overlapping the marker) and misses base64, hex, upper-case and separator-split leaks; a honeypot call with non-JSON arguments is not logged.
What you must do, specifically:
1. canary.py: start the 8-character window inside the secret part only; add the encoded forms (base64, URL-safe base64, hex, case, separators) or document the limit in the README.
2. honeypot.py: make arguments_digest total (default=repr) and append before anything can fail; test that the call is logged once.
3. Answer items 4 to 7 of my review (state_digest, file modes, descriptions, realistic decoys) on #256.
Needed by: next head of #256
