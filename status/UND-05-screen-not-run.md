# UND-05 · SCREEN_NOT_RUN, the internal class of a screen that did not run (A437)

Core Engineer 2 (C2), 20 Nov 2026. Task UND-05; REQ-CTX-007, REQ-AUD-001, REQ-INT-002; INV-02, INV-19, AGENT-09, AGENT-13, SoT §29. Follow-up of noema#792. Ruling A437 (`docs/decisions/DEC-2026-11-20-architect-rulings-a358-a453.md`, interim until a non-author Founder approves it): "#792: the internal class is SCREEN_NOT_RUN. The public error stays PROVIDER_UNAVAILABLE, so there is no contract change."

## What changed
- `pipeline/types.py`: `ScreenStage` (`UTTERANCE`, `CANDIDATES`), and `PipelineResult.screen_not_run: tuple[ScreenStage, ...] = ()` (defaulted, so every other construction is unchanged).
- `pipeline/run.py`: `_stopped(..., screen_not_run=...)`. The utterance screen is the last of the five parallel steps; if it is among the refusals it is named, whatever else was refused (the turn keeps the first refusal's code). The candidate screen is named at its own rejection.
- `compiler/service.py`: `ScreenNotRun(ProviderUnavailable)` with `stages`. `compiler/compiler.py`: a rejected turn with `screen_not_run` raises it; tenant-scope and input-invalid still win; every other rejection is raised as before.
- The route catches `ProviderUnavailable`, so the public answer (502, `PROVIDER_UNAVAILABLE`, "No provider gave a usable answer.", retryable) is byte for byte the same; a test pins that and that no internal word is in the body.

## Not built, and why
- **No log line or metric.** The compile path has no logging or metrics infrastructure; a new one needs the redactor (AGENT-13). The class is on the result and the exception.
- **The recorder's `failure_class` vocabulary is not widened.** It is closed (noema#779) and read closed by UND-15's loader (`benchmark/compile_acceptance/replay.py`); the failed screen call is already identified by its route `decide.instruction_likeness`.
- No contract, core, setting or dependency change. `gate_errors` is untouched, so the tests of noema#792 stand as written.

## Tests (tests first; red `9662e668`)
`tests/test_pipeline_screen_not_run.py`: 16 tests. 10 hand mutants of the two marking sites, the compiler's raise, the subclass and the stages, all killed (the marking at the first step instead of the last; always or never marking; the candidate site naming the utterance or nothing; the compiler never raising or raising for every rejection; `ScreenNotRun` not a `ProviderUnavailable`; the stages dropped; marking only for a single refusal).

## Pre-work for the next agent
- Exact next step: if a log or metric is wanted for the internal class, it needs the model service's redactor and an enum-only label (`SCREEN_NOT_RUN` plus the stage) first; that is its own change.
- Do not touch: the recorder's manifest vocabulary without UND-15's loader and `noema#778`.
