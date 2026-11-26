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
- Conflicts raised: none blocking. Two items for the Architect in the PR body: an added field is UNKNOWN here and "Ignored" in REQ-CTX-008; a stale answer of a state that was final cannot be told from a live one.

## Evidence
- Characterisation: the code under test already existed, so there is no behavioural red. The red run is the missing recordings (the drift guard and the core tests fail until `read_hostility_recordings.json` is committed); mutants show the tests bite.
- Mutants on production code (17 hand mutants over the adapter, the reader and the status function): 15 killed; 2 survive and are equivalent for this suite (the provenance check inside `answered`, which a well-formed adapter answer never trips, and the source's own `unavailable: true` flag, which the proxy does not produce; both have their own tests in `portread`). Mutants on the proxy (clock skew shifts nothing, unknown_enum changes nothing): both fail the drift guard.

## Open questions
- Retry-After and the [REC] 100, 300, 900 ms backoff have no implementation to test: no read retries exist and the retry budget is OD-P-112 (AGENT-16). Owner: Architect (budget), then whoever builds the read budget (REQ-CTX-008 "within the read budget").
- The `EVIDENCE_READ` attempt count that REQ-CTX-008 names is not recorded anywhere.
- A transport that replays a dropped GET once (Go's own behaviour on a reused connection) makes the attempt count not equal to the number of adapter calls.

## Pre-work for the next agent
- Exact next step (the Tushe-contract part): wait for ENV-03 and ENV-05's list; then show the same handling against Tushe's documented read behaviour, with no behaviour assumed that has not been shown (AGENTS section 14).
- Files to read first: `services/core/evidence/readhostility/README.md`, `benchmark/hostility_proxy/README.md`.
- Do not touch: `testdata/read_hostility_recordings.json` by hand; re-record it.
