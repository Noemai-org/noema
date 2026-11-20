
# Session 2026-11-20 · SEC-07 scanners on every surface, cross-scope detection, planted leaks (slice 1)

## Session summary
- What changed: `tools/ci/scopescan.py` (cross-scope scanner), `tools/ci/sec07_nightly.py` (the driver: planted leaks on every surface, then the scan, then the counters), `tools/ci/sec07/` (a synthetic registry and the five-surface selftest), `.github/workflows/sec07-nightly.yml` (nightly probe), one step in `safeguards.yml`, a README row, 42 tests.
- Why: SEC-07 (plan: "scanners on logs, traces, API output, explanations and the Record: PII, secrets, full account numbers, cross-scope leaks; feeds `noema_cross_scope_disclosure_total`"; done when "scanners run nightly; planted leaks all caught"; needs SEC-03, merged). `leakscan.py` (SEC-03 stub) covers digit runs, card numbers, secrets and canaries on one directory; it knows nothing about scopes, and nothing planted a leak per surface.
- How it detects cross-scope: by TOKEN. Each scope (a tenant, a principal nested in it) has ids and a planted high-entropy marker; a surface of scope S may hold the tokens of S and of scopes nested with it, and of nothing else. A stream of several scopes (a log) is judged line by line by its own `tenant_id` member; a line with no usable scope holds at most one scope's tokens. Word boundaries and longest-token-first matching stop a token from matching inside a longer one.
- Done in this slice: the scanner, the planted-leak probe on every surface kind, the metric in Prometheus text form, the nightly selftest workflow, the CI tests.
- NOT done, and why (so nobody reads this as SEC-07 complete): (1) the scan of a PERSISTENT target's real surfaces: needs a persistent stack and its logs and traces collected nightly (IL); (2) the Record scanner as an AUDITOR: a read-only command that exports each tenant's events through an auditor login into the Record surface files (C1/EL: the login and the reconstruct readers are theirs; I will review it); (3) the counters reaching the core's own `/metrics` (REC-02 telemetry is an empty boundary today): until then the counter is the file the job writes and a non-zero value fails the job.
- Risks, and how they are contained: a scanner that finds nothing reads as clean. Contained by the probe (every planted kind on every surface or exit 3), by exit 2 for nothing scanned, and by 26 mutants of the scanner and the driver, each caught.
- Tests added (REQ-SEC-003, REQ-SEC-004): `test_scopescan.py` (23), `test_sec07_nightly.py` (19). Mutation checks: 26 of 26 caught (the first run left 4 survivors, each now pinned by a test).
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: IL exports the persistent stack's logs and traces (each line with its `tenant_id`) and the API and explanation answers per principal into per-surface folders and writes a `surfaces.json`; C1 or EL writes the auditor export of the Record; then one `sec07_nightly.py` run per night with `--metric`.
- Do not touch: the planted-value generator (`g1_leakscan.planted_values`), shared with the G1 workflow.
