# Status · EVD-05 (Core Engineer 2) · read hostility, the hostility-proxy part

Updated 2026-11-26. Plan task EVD-05 (owner C2+AG; REQ-CTX-008). Task text (plan v1.30): "Read hostility: `Retry-After`, backoff, full pagination, schema drift to UNKNOWN, near-timeout handling. Two parts (A717, CR-2026-11-25-evd-05-hostility-proxy-split): the hostility-proxy part (the proxy is ours) starts now, ENV-03 waived for it; the Tushe-contract part keeps ENV-03." Done when: "Hostility fixtures pass". EVD-05 is **not Done**: only the hostility-proxy part is worked here; the Tushe-contract part waits for ENV-03 and ENV-05's list of unstated behaviours.

Order of the work: A717 of `DEC-2026-11-26-architect-rulings-a704-a720` ("The ENV-03 waiver is ruled for the hostility-proxy part only (the proxy is ours). The Steward records it. C2 starts now."), applying the draft `CR-2026-11-25-evd-05-hostility-proxy-split`.

# Session 2026-11-26 · EVD-05 · the Evidence read path under the hostility proxy's faults

## Session summary
- What changed (PR links): see the pull request of branch `test/EVD-05-hostility-proxy`. New test-only package `services/core/evidence/readhostility/` (doc, README, replay harness, tests, `testdata/read_hostility_recordings.json`); `benchmark/hostility_proxy/internal/proxy/record_read_hostility_test.go` (the recorder and its drift guard) and one README row. No production code, contract, pack, dependency, migration or workflow change.
- Why: REQ-CTX-008 asks that a read adapter handle 429 with and without Retry-After, pagination, schema drift, near-timeout answers and clock skew, with the acceptance criteria "no decision treats a partially read paginated set as complete" and "unknown enum values never map to known states"; REQ-TST-004 and AGENT-12 say the faults come from the proxy or from Tushe's FaultPlans.
- Risks, and how they are contained: tests only. The one behaviour a test server supplies is the timing of a recorded transport fault (the hold and the cut connection); the proxy's own tests show the proxy does it, and the recording carries the hold it asked for.
- Tests added (REQ-CTX-008, REQ-CTX-002, REQ-TST-004, REQ-ENV-004, INV-07, INV-19, INV-23, INV-01): 22 fault families by 3 reads (66 recorded exchanges), replayed to the real `StandinClient` and `portread.Reader`; plus the no-model-in-the-path scan, the recording's reproducibility, and the upstream's wire-shape guard.
- Regression cases added: none (no defect fixed).
- Debt entries: none.
- Conflicts raised: none by me. A728 (`DEC-2026-11-26-architect-rulings-a721-a737`) answered the five questions: an added field reads as UNKNOWN (CONFLICT SOT-C-35, REQ-CTX-008 "Ignored" against the safer reading, applied meanwhile); a stale read of a state is caught at dispatch revalidation (REQ-EXE-003) and by the A706 version token, not at read time (recorded known limitation; guarding tests `execution/e2e` `TestG2ARouteGrantBoundToAStaleTokenIsVoidedAtRevalidation` and `execution/bridge` `TestRouteRevalidation`; the A706 token is not built, so no test of it); reads use a fresh connection each time; the read retry budget stays 0 beyond EVD-02's one refresh (OD-P-112 Open); the stand-in's identifiers are fixed by IL and the reader is never loosened.

## Evidence
- Characterisation: the code under test already existed, so there is no behavioural red. The red run is the missing recordings (the drift guard and the core tests fail until `read_hostility_recordings.json` is committed); mutants show the tests bite.
- Mutants on production code (17 hand mutants over the adapter, the reader and the status function): 15 killed; 2 survive and are equivalent for this suite (the provenance check inside `answered`, which a well-formed adapter answer never trips, and the source's own `unavailable: true` flag, which the proxy does not produce; both have their own tests in `portread`). Mutants on the proxy (clock skew shifts nothing, unknown_enum changes nothing): both fail the drift guard.

## Open questions
- Retry-After and the [REC] 100, 300, 900 ms backoff have no implementation to test: no read retries exist and the retry budget is OD-P-112 (AGENT-16). Owner: Architect (budget), then whoever builds the read budget (REQ-CTX-008 "within the read budget").
- The `EVIDENCE_READ` attempt count that REQ-CTX-008 names is not recorded anywhere.
- For EL (production, not edited here): `services/core/adapters/synthetic_tushe/standin_client.go` line 87, `hc = &http.Client{}` (when `StandinOptions.HTTP` is nil), keeps connections alive, so Go's transport replays a dropped GET once. A728 wants a fresh connection per read: `hc = &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}`. The tests here pass that client in `StandinOptions.HTTP` and assert each read reaches the source exactly once (red first at 50bb888ee). Whoever wires `StandinClient` in `cmd/core` must pass the same client until the default changes.
- Identifiers (IL): the recordings use `TX1` and `usr_ab12` because `portread` rejects the stand-in's `fin.transaction:TX1` and `usr_a`. The stand-in fix is not on main (`benchmark/fixtures/test_standin.py` still has them as of the merge at ee737f9f1); switch the recordings to the stand-in's valid references when it lands.

## Pre-work for the next agent
- Exact next step (the Tushe-contract part): wait for ENV-03 and ENV-05's list; then show the same handling against Tushe's documented read behaviour, with no behaviour assumed that has not been shown (AGENTS section 14).
- Files to read first: `services/core/evidence/readhostility/README.md`, `benchmark/hostility_proxy/README.md`.
- Do not touch: `testdata/read_hostility_recordings.json` by hand; re-record it.
