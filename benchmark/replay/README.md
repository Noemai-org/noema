# benchmark/replay

**Implements:** REQ-BEN-002, REQ-NFR-001 (task BEN-05: the G1 evidence run). **Owning track:** T4 (benchmark owner). **Updated:** 2026-10-31.

The replay diff. Given two runs of the same cases through Noema's public API, it reports whether the Trust decisions and the verdicts are identical (INV-21). It is a pure function of recorded files: it runs nothing, calls no model and reads no oracle. Generated layer only; it refuses any argument or environment value that names `benchmark-heldout` (INV-22, REQ-AGT-004). SYNTHETIC data (AGENT-25).

```
python -m benchmark.runner --suite S --out RUN_A --run-id <id-a> --commit-sha <sha> ...    # first run
python -m benchmark.runner --suite S --out RUN_B --run-id <id-b> --commit-sha <sha> ...    # second run, a separate run with its own ID
python -m benchmark.replay  --suite S --first RUN_A --second RUN_B --out OUT [--story US-01 --story US-06] [--exclude-mutated [--open-exclusions FILE]]
```

Output: `OUT/replay.json` and `OUT/replay.md`, byte-identical for the same inputs.

Exit codes: **0** IDENTICAL; **1** DIFFERENT or NOT_COMPARABLE (the report is written); **2** an input is unusable or the two runs cannot be compared (nothing is written); **3** a run is NOT_RUN, no access (nothing is written, nothing is faked).

## Rules the tool enforces

- The two runs must differ in `run_id` and agree on the commit (and it must be recorded), the suite digest, the generator version and seed, the pack version and the policy version. Anything else is a refusal (exit 2), not a comparison.
- Every result file must validate against `benchmark-case-result.v2` and belong to the run whose manifest sits beside it.
- **An errored case, a case missing from one run, or a story with no comparable case is NOT_COMPARABLE, never identical.** Two runs that fail the same way prove nothing about a decision.
- A difference outranks NOT_COMPARABLE. The verdict is IDENTICAL only when every selected case is compared and equal and every story has at least one compared case.
- Compared: disposition, classification, resolved target, clarification and escalation flags, audit event types (in Record order), dispatches with their grant state, verdicts, claims, disclosed refs, environment effects, the three environment counters, the outcome digest and the number of customer turns the runner sent (`turns`, benchmark-case-result v2). Dispatches, verdicts, claims, refs and effects are compared as sets of rows. Latency, cost and run ID are not compared.

## Selection (founders' decision of 1 Nov 2026; tightened by the Architect on 2 Nov after Codex's finding)

The default selects every case whose manifest `covers` names a requested story, mutated ones included. `--exclude-mutated` removes **only the cases named in `open_exclusions.json`** (the reviewed registry beside this module, or the file given by `--open-exclusions`). It never removes a case by kind: an unnamed mutated case stays in, and an errored or missing story-covering case, named or not by anyone else, makes the verdict NOT_COMPARABLE. A registry entry needs a reason, who holds the blocking fault and the date it was opened; an entry for a case the suite does not contain is refused (exit 2).

Today the registry names one case: `BEN-CLEAN-1007` (US-01 with `INJECTION_IN_NAME`), which needs `mock-customer INJECTION_PAYLOAD`, a Tushe-held fault that is not wired. The report states "covers 5 of 6 cases" and lists the case under **Open exclusions** with its fault tag, holder, date, status OPEN and reason. Every named case is still probed: one that can be compared is compared and counted (it cannot hide a difference, and the exclusion closes itself), and one that cannot stays listed with what was observed in the two runs (`observed`). While any exclusion is open the report says "IDENTICAL on 5 of 6 cases" and `replay.json` has `full_coverage: false`; a reader must not take the verdict as covering the case. An exclusion is never a pass, and the case stays in the G2 and G3 runs. Removing an entry is how an exclusion is closed. Detail: `status/BEN-05.md`.

## What the diff does not cover

The case result holds the Trust disposition, the dispatches and their grant state, the verdicts and the environment effects. It does not hold the Trust decision payload or the Record events. The report states this in its Limits section.

## Tests

`make bench-test` (unit tests and `mypy --strict`). `test_compare.py` covers the comparison; `test_cli.py` covers exit codes, files, byte-stability and the refusals.
