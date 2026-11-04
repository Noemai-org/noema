# Isolation refusal check (SEC-02)

Date: 2026-11-04 · Owner: Security Reviewer · Status: built against a test double; Tushe's refusal status and body are not yet documented.

Proves AT-02 / INV-22 from the runtime side: with the **runtime key**, every `/partner/...` route and every monetary mutation is refused with a 4xx and no data body.

- `check_isolation.py`: the check (standard library only). `judge()` is the pass rule: 4xx, body empty or an error envelope whose fields are only `error`, `code`, `status`, `request_id`, `type`, each a short code token (no free text, no run of six or more digits). `message`, `detail` and `title` are refused: they can carry sensitive data while passing a shape check. A 2xx/3xx/5xx, a list, nested data, a large or non-JSON body, or no answer is a FAIL. Until Tushe documents the refusal, any such 4xx passes; set `EXPECTED_STATUS` when it does.
- `probes.json`: the probes. `/partner/events` is named by the signed G0 worksheet; the monetary paths are **candidates** (Tushe has not documented them) and are replaced when it does.
- `test_isolation.py`: runs the check against a SYNTHETIC double. It includes eight leak mutations (served, 4xx with data, nested data, 5xx, redirect, large body, non-JSON, one route leaking) and an unreachable-server case; each must fail.

Run the tests: `cd benchmark/isolation && python3 -I -m unittest -v`.
Run against an environment: `python3 -I check_isolation.py --base-url URL --key-file FILE`.

The Tushe admin credential is never used by this check or held by an agent. Against Tushe itself the check runs only when the Founder-attached runtime key file exists; until then the result is "unknown", not "pass".
