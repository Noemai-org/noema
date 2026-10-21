# tests/fixtures/providers

**These are synthetic fixtures, not recordings.** No vendor API key was available to session D, so each file is written by
hand from the vendor's published response shape (message content, finish reason, token usage, model version) and then
extended with the awkward cases the adapters must survive: a reasoning block that must be dropped, a refusal, a cut-off
reply, a non-object body. Nothing here came from a live call.

Status of the contract tests that read them: **not run against a live endpoint: no access.** When a key is available,
record a real response per adapter, replace the matching file here, keep the extended cases, and run a live smoke test
(REQ-INT-002, Tests). Until then a vendor wire-format change is not detected by this suite.

Every identifier in these files is invented, and the credential in `../credential.txt` is a placeholder.
