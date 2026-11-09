# benchmark/impact

The IMPACT layer of the benchmark suite definition v1: resolution time, human investigation minutes, human touches, escalation rate, first-contact resolution, SLA exposure, cost per exception, backlog and customer effort. It is a **modelled** layer, not a case family: it is computed from the results of RESOLVE and RESIST under a published Operations Model, identical for every arm, with M0 (a manual baseline) beside it. Every figure is labelled "modelled" and shown across a sensitivity range.

| File | What |
|---|---|
| `operations-model.v1.json` | the Operations Model. **Every parameter is ASSUMED**: a placeholder with no source, so that the computation can run. The Founder supplies the real values and their sources; a parameter then becomes `SOURCED` with a non-empty `source`. Contains the escalation table (one row per routing destination in the pack, plus the clarification hand-off), the M0 baseline, the formulas and the sensitivity method |
| `test_operations_model.py` | keeps the file honest: an ASSUMED parameter has no source, low ≤ value ≤ high, the escalation table matches the pack's `routing_allowlist`, every IMPACT metric has a formula |

Not built: the computation itself. It needs the arm result schema, the scorer's per-case outputs and the Founder's values. Nothing here is a claim about any institution (AGENT-18). The metric definitions are in `benchmark/scoring/METRICS-v1.md`; the specifications are in `benchmark/generator/SPEC-suite-v1.md`.

Run the tests: `python -m unittest benchmark.impact.test_operations_model`.
