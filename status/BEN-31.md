# Status · BEN-31 (Benchmark Owner agent) · public benchmark format and publication rules

Updated 2026-11-03. Owner: B with the Founder. Needs: BEN-16 (not on `main`; checked 3 Nov). REQ-BEN-010. G7.

## State

- **Built:** `benchmark/publication/`: the format and ten rules (`README.md`), and `python -m benchmark.publication check|claim`, which enforces R1 to R8 against an evidence bundle of scorecards. Tests first (27, seen failing).
- **Why before BEN-16.** The rules and the check need a scorecard, not the full suite; any run's scorecard (v1 or v2) works. The "n, method, bound, CI on every number" rule is the part G7 needs and it is machine-checked.

## Open

1. **G7 checklist reference.** Done-when also says "G7 checklist references them". The checklist is `docs/poc/execution/gates/G7.md` in `Noemai-org/docs`; a one-line pointer to `benchmark/publication/README.md` belongs there (a docs pull request, small). Not done in this pull request (another repository).
2. **R9, R10** are review items, not checks (demo scripting, ROI and compliance claims). Stated in the README.
3. **Cherry-pick check covers families and categories only.** Other slices (by arm, by provider) will need their rows added when those comparisons exist (BEN-24); one line each in `SLICES`.

## Messages

### 2026-11-03 (time not recorded) accepted · Founder (work order, relayed by the user) → Benchmark Owner agent
- Purpose: BEN-31 (publication format and rules) after BEN-29, TST-06 and UND-13.
- Receiver used it for: this change.
- Evidence: the PR for this branch.
