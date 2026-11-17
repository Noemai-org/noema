# Session 2026-11-06 14:30 WAT · UND-13 (model service side)

(Separate file from `status/UND-13.md` only to avoid add/add conflicts with #199 and #212; the Steward may merge them.)

## Session summary
- What changed (PR links): noema#215, `services/model/noema_model/qualification/` (canary, gate) and `harness/canary_asker.py`, with golden fixtures made by BO's #199 code at a3fe3ad.
- Why: REQ-INT-006, D25, AGENT-28; the 6 Nov tracks 3 and 4.
- Risks, and how they are contained: depends on #199's formats (do not merge before #199); a changed record shape fails the golden tests on purpose. #212's `contracts/schemas/qualification-record.v1.json` now disagrees with #199's record format (same name, different fields): recommend closing #212.
- Tests added (REQ, INV, US or EC IDs): 37; REQ-INT-006, INV-22. Mutation check: seven real mutants killed (drift outranks unavailable, version mismatch, case layer, bugs not swallowed, non-object record file, same-day disagreement, unpinned).
- Regression cases added: none (new code).
- Debt entries: none.
- Conflicts raised: #212 vs #199.

## Pre-work for the next agent
- Exact next step: wire `require_qualified` at service start (needs a records directory and a canary timeout in configuration: a Founder/BO decision, AGENT-16); regenerate the fixtures when #199's A17 record lands.
- Open questions (owner): canary deadline and subset location (Founder); close #212 (Founder).
- Files to read first: benchmark/qualification/README.md on #199; services/model/noema_model/qualification/README.md.
- Do not touch: benchmark/ (BO).

## Session 2026-11-07 · rulings A36 and A29 (model service half)
- Fixtures regenerated from #199 at 216c939 (the A17 record: `drift` and `bar` blocks, five runs, drift PASS, bar), and the lookup now fails closed: any invalid record for the identity refuses it (the skipped record might have been the revocation). Red commit first (fixtures and the test change), then the gate. Five mutants killed (fail-closed, below-bar, drift, no-bar, five-runs).
- #212 closed (A36). The pin rule is the harness's own (`check_pinned`); BO's `pins.py` now matches it and says a golden test here compares them (the gate pin cases).

# Session 2026-11-08 · UND-13 slice 2 · EL's blocking finding on noema#215

## Session summary
- What changed: `qualification/gate.py` (a rate, floor or baseline must be a finite int or float, not a bool, within 0..1; `fullmatch` for the digest and the date; a non-string reason invalidates the record), `qualification/canary.py` (`timeout_s` must be a positive finite number), README (nothing calls the gate yet; the NaN note; the import line).
- Why: EL's reviewer-role statement at ddb9be1: a NaN rate added no reason (every comparison with NaN is false), so a malformed record could pass the gate. I had posted "READY FOR CODEX" over that open finding; it was premature and is corrected here.
- Tests added (REQ-INT-006, AGENT-28): `tests/test_qualification_gate_malformed.py` (eleven bad rates, ten bad floors and baselines, trailing newline, four unhashable or non-string reasons) and the canary deadline cases. Red first (commit 89af9249).
- Not done: the same weakness is said to exist in the benchmark module (noema#199, Benchmark Owner's code): reported by relay issue. Wiring `require_qualified` into start-up is the next slice. Per-case reasons for UNAVAILABLE are a later change.
- Debt entries: none. Conflicts raised: none.

# Session 2026-11-15 · UND-13 (C2 half) · the port to the final record of noema#199

## Session summary
- What changed (PR noema#215): `gate.py` and `canary.py` follow `benchmark/qualification/record.py` and `run.py` as merged in noema#199 (see the README of `noema_model/qualification`). The golden fixture `gate_golden.json` was regenerated from the benchmark module's own `build_record` and `check_model` (84 lookups over 82 SYNTHETIC records; the old fixture had 24 over 14).
- Why: noema#199 added `runs`, the `floor`/`floor_source` pair, the baseline lineage by digest, the 40-case minimum, the scoring context, `pass_k_cases_sha256`, and the rate rules; the gate here mirrored the older A17 record.
- Risks, and how they are contained: the contract is checked by hand (no validator, AGENT-14); the differential test and 47 hand mutants on the gate and canary: 46 killed, 1 equivalent (comparing canonical YYYY-MM-DD dates as strings gives the date order) hold the two modules together; stricter than the schema only where a float is not an integer.
- Tests added (REQ-INT-006, AGENT-28, REQ-BEN-009): the golden differential (84 lookups), the canary's subset and baseline consistency tests.
- Regression cases added: a revoking record of an invalid kind still refuses; an A17-shaped record (no `runs`) refuses; a bar whose baseline record is missing, invalid, another model or version, or measured in another context refuses.
- The Voice Engineer's independent read of noema#215 at `01cca65f` (READY, four notes, 16 Nov 2026): note 1 (a defect: `_SHA.match` with `$` accepted a trailing newline) and note 2 (the baseline's own `commit_sha` was not validated) fixed, tests first (`4c0e0fa9`, 14 cases red): `_is_sha` (a string and `fullmatch`) for the run's and the baseline's SHA, refused before any call; notes 3 and 4 are README statements (a record's date is not a security control; a speech model needs a version identifier from Intron). The golden fixtures are unchanged.
- Debt: not ported: the understanding-dependent check of subset cases (the benchmark's family list is not in this service).
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: wire `require_qualified` and `run_canary` into the service's start-up (the rest of UND-13's done-when); records live in `benchmark/qualification/records/`.
- Files to read first: `noema_model/qualification/README.md`, `gate.py`.
- Do not touch: `benchmark/qualification/records/` (CODEOWNERS).
