# Session 2026-11-23 · UND-18 (C2)

## Session summary
- What changed (PR links): noema#932. The optional closed-vocabulary slot `requested_remedy` is asked as a yes/no gate (one fixed sentence, `SLOT_GATE_V1`) and, only when the gate's probability of yes is at or above the pack's threshold, picked with no abstention (`SLOT_PICK_V1`, `ChoiceQuestion.pick_only`). The threshold is pack data: `understanding.slot_gates`, `{slot: requested_remedy, assertion_threshold_bp: 9000}`. 9000 is the Architect's interim value (A603) and a Founder decision to confirm.
- Why: run 7 (45268856727), goldens G-A, G-B, G-E missed on `requested_remedy`; A603, A605. No golden and no input is edited (A533); the optional-slot wording of #901 is unchanged for slots that are not gated.
- Risks, and how they are contained: the gate compares the probability as the decimal the provider wrote, exactly, against basis points; a model's own "yes" at a low probability counts as doubt (`gate_asserted`, unlike `asserted`); only an answer outside the pick's options is "absent", any other refusal still stops the turn.
- Tests added: `services/model/tests/test_remedy_gate.py` (38), `tools/packcheck/test_packcheck.py::SlotGates`, ten contract fixtures.
- Regression cases added: the run-7 failure shape is `test_the_gate_passes_exactly_at_and_above_the_pack_threshold` and `test_a_models_own_yes_at_a_low_probability_is_doubt_and_abstains`.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: A602 (the job log prints, per golden, the option chosen, the top five probabilities and the threshold applied) reads `slot_probs` (`requested_remedy_gate`: `{yes: p}`) and `config.slot_gates`. A604: fix the gate against BO's labelled set with the replay provider before any live run.
- Open questions (owner): the threshold value (Founder).
- Files to read first: `understanding/pipeline/steps.py` (`gate_slots`, `pick_gated`, `gate_asserted`), `understanding/pipeline/run.py` (stage 2b).
- Do not touch: the goldens, the inputs, the wording of the non-gated optional-slot question.
