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

## Session 2026-11-24 · UND-18 (C2), A649 and the review's P2
- A649: the gate's probability is read from the raw JSON number token. `harness/rawjson.py` decodes with `RawNumber`/`NonNumber` and makes no float; `GateQuestion`/`GateAnswer` carry the token; `probability_bp` accepts only a `RawNumber` holding a plain decimal in [0, 1]. A missing, null, quoted, boolean, signed, NaN, Infinity or exponent-form probability abstains for that slot; invalid JSON still refuses the reply (ProviderUnavailable). A set with a gate is never sent to a decision provider (its reply is parsed to numbers at the vendor envelope), so it goes to the LLM route.
- P2: a gated slot name declared by more than one node is refused (packcheck, the loader and `PipelineConfig`).
- Next step: A602 reads `proposal.probabilities["requested_remedy_gate"] == {"yes_bp": n}` (present only when the token was a plain decimal).

# Session 2026-11-25 · UND-18 (C2), EL's repeated-option finding

## Session summary
- What changed (PR links): noema#932. EL's review (CHANGES NEEDED, important): in `_decode_decision` a gate answer with two `yes` entries let the last one win, so `yes 0.10` then `yes 0.95` gave the token `0.95` (9500 bp, at or above 9000) and asserted the slot. Reproduced first (16 red cases, CI `model` job 139291465833, read whole). Fix: a repeated option in a gate answer (`yes` or `no`) refuses the WHOLE reply with `HarnessRejection(Reason.OUTPUT_MISMATCH)`, the rejection `_answers` gives for a repeated option on the ordinary path (INV-02: whole-reply, strict, no repair). Chosen over "the slot abstains" because the ordinary path refuses the reply, a reply that contradicts itself is not a reply to read, and it needs no new rule; it fails closed (the compile reports a refused reply, never a slot).
- Why: a false assertion is the harmful error; contradictory input must fail closed.
- Risks, and how they are contained: a reply with a repeated option used to be accepted and now refuses the whole decide call (the same as a repeated option on any other question); nothing that was a plain, single-entry reply changes. The A649 behaviour is untouched (9e-1, quoted, null, missing, signed, NaN and Infinity abstain for the slot only; invalid JSON still refuses the reply): the tests of that table pass unchanged.
- Tests added (REQ-INT-005, REQ-AMB-002, INV-02): `test_remedy_gate.py`: 14 repeated-option cases, a `no` selection with a repeated yes, the harness-level reason, a repeat on a non-gate question (unchanged), the single-yes controls, two gate answers in one reply.
- Regression cases added: the same tests (the EL scenario is the first parametrised case).
- Debt entries: none. Conflicts raised: none.
- Nit: the docstring of `_decode_decision` now says the `no` entry's `p` is not validated (only the option name; only `yes` is read).

## Pre-work for the next agent
- Exact next step: SR and Codex re-read the final head; A602 and A604 wait on this merge as before.
- Open questions (owner): the threshold value (Founder), and `pack.v1` gains an optional member without a version bump (Founders, see the PR body).
- Files to read first: `services/model/noema_model/harness/rawjson.py`, `harness/core.py` (`_decode_decision`, `_answers`, `_native_request`), `understanding/pipeline/steps.py` (`probability_bp`).
- Do not touch: the goldens, the inputs, the threshold (9000), the recordings.
