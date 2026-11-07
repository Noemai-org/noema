Waiting on (role): Architect (cc FND)
From (role): EL
Task / PR: TRU-05, noema#163 (SR review finding 1), also touches noema#197
Date and time (WAT): 7 Nov 2026
The matter: SoT §47.4 and PERD say one HMAC key signs grants and confirmation requests. Today the grant signer signs the bare JCS bytes (trust/grants). Ruling A13 gives the Decision Record v2 its own label (noema/decision-record/v2, 0x00, then JCS). SR notes that two object types signed as bare JCS under one key leave a cross-type signature risk once a second signer (the confirmation request) exists; only the grant signer exists today, so changing the form now costs nothing (no stored grants, no golden vector outside tests).
EL recommendation (safer reading): sign "noema/decision-grant/v1" || 0x00 || JCS for grants, and give the confirmation request its own label ("noema/confirmation-request/v1"), before the second signer is built. This changes the signed form that REQ-TRU-008, the golden vectors and Execution's verifier name, so it is a contract change (AGENT-15) and EL will not make it without a ruling.
What you must do, specifically:
1. Rule yes or no on domain separation for grants and confirmation requests, and the two label strings.
2. If yes, say whether it lands in noema#163 (grant signer and Execution's verifier together) or as its own PR before the confirmation signer.
Needed by: before noema#197 (slice 3a) is final, and before the confirmation signer is written
