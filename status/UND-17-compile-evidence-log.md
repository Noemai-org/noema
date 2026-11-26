# Session 2026-11-24 · UND-17 (A602: the compile run's evidence is in the job log)

## Session summary
- What changed (PR links): noema#931. The compiler (`PipelineCompiler`) takes an optional diagnostics sink and hands it, per turn, the closed-vocabulary choices as `SlotChoice` (slot, option chosen or none, probability of every option, threshold applied). The benchmark's `LiveBackend` files them by example and prints one line per slot per golden: `CHOICE <case> slot=<slot> chosen=<option> top5=<option>:<p>,... threshold=<p or n/a>`. The line goes to the job log (stdout), into `summary.md` (a section after the table, shown by the workflow's Summary step after a clean scan) and onto `CaseResult.evidence`.
- Why: run 7 (45268856727) failed G-A, G-B and G-E on `requested_remedy` and the artifact host is unreachable for agents, so the run was read blind. A602: stop tuning against the goldens; make the next run readable from the job log.
- Risks, and how they are contained: the sink is internal and optional; the proposal and the route's answer are the same with and without it (a test compares the bytes). A probability prints as four decimals (`0.7800`), never as basis points: 10000 would be a five-digit run the leak scan hides. An option id outside the shape of a pack id prints as `invalid_id`, so no word of the request can ride in an option name. The threshold is `n/a`: no threshold is applied to a choice today (the pack's threshold applies to yes/no answers); A603 gives the gate its own.
- Tests added (REQ-INT-001; INV-22; AGENT-13, AGENT-16): `benchmark/compile_acceptance/test_choice_evidence.py`, `services/model/tests/test_compile_diagnostics.py`, two cases in `services/model/tests/test_compile_acceptance_build.py`.
- Regression cases added: the same.
- Debt entries: the record-only inputs print no choice lines (the Founder asked for the goldens; "if cheap" was not cheap: it needs a second result type). A later PR can add them.
- Conflicts raised: none. No golden, input, prompt wording, threshold or pack value is touched (A533, A602).

## Pre-work for the next agent
- Exact next step: A603 (a yes/no gate, then a capability pick with no abstain option; the gate's threshold in pack data, interim 9000 basis points, a Founder decision to confirm), then A604 (the gate against BO's offline labelled set with the replay provider). The threshold of the gate goes into `SlotChoice.threshold` so this line prints it.
- Open questions (owner): none.
- Files to read first: `benchmark/compile_acceptance/evidence.py`, `services/model/noema_model/understanding/compiler/compiler.py` (`SlotChoice`, `_choices`).
- Do not touch: the goldens and the inputs (A533).
