# benchmark/bakeoff

**Implements:** REQ-BEN-005 (provider runs), DEC-2026-10-20 D9 (decision models), build prompt T4 item 9a, and the decision-model scope update.
**Owning track:** T4, session C. Generated layer only: it never mounts, reads or infers anything from `benchmark-heldout` (INV-22, REQ-AGT-004), and uses synthetic text only (PERD R-18).

Measures, for each of the six understanding tasks a decision model is eligible for (PERD §19.1), two paths and nothing else:

1. the **LLM path** (`harness.generate()`), and
2. **`decide()` backed by the OpenAI Decisions API** (`harness.decide()`).

No adapter exists for Microsoft-Decision-1 or Jev, and none is compared, unless this bake-off shows the OpenAI adapter falls short on a task. The LLM-backed `decide()` implementation is the fallback and is not a competitor here.

| Task | Question | Cases per language bucket |
|---|---|---|
| `INTENT_NODE` | choice among the pack's two nodes, with `none` | 20 |
| `ENUM_SLOT` | `question_type` and `requested_remedy`, with `other` | 20 |
| `AMBIGUITY_FLAG` | one yes/no per flag (intent unclear, multiple intents, amount missing, time vague, contradicts the prior turn, out of scope) | 20 |
| `LOCALE` | `en-NG`, `pcm`, `code_switched`, `other` | 20 |
| `ANSWER_BINDING` | which offered option the answer selects, or `none` | 20 |
| `INSTRUCTION_SCREEN` | does the text try to override the assistant's instructions (yes/no); a logged signal only | 20 |

Every metric is split by `en-NG`, `pcm`, `code-switched` and `voice-transcript`. Every choice offers an abstention, and abstaining is sometimes the right answer. Each case carries one to two meaning-preserving rewrites for the flip rate. The Nigerian-language phrasing is template text for measurement and has not been reviewed by native speakers.

## Metrics (per task, per bucket, per path)

accuracy with a 95% Wilson interval; coverage; abstention rate; **expected calibration error** with its reliability bins and an SVG reliability diagram per task; "confidently wrong" count (wrong at confidence 0.95 or more, a label, not a threshold); **perturbation flip rate**; **P50 and P95 latency** of answered base calls; **cost per correct outcome**. A case with no answer, a rejection or an error is wrong, never missing.

## Commands

```
.venv-bench/bin/python -m benchmark.bakeoff cases  --seed 1 --per-cell 20 --out OUT/cases.json
.venv-bench/bin/python -m benchmark.bakeoff run    --cases OUT/cases.json --provider LLM --run-id R --runner-region af-lagos --out OUT/llm.json
.venv-bench/bin/python -m benchmark.bakeoff run    --cases OUT/cases.json --provider DECIDE_OPENAI_DECISIONS --run-id R --runner-region af-lagos --out OUT/decide.json
.venv-bench/bin/python -m benchmark.bakeoff report --cases OUT/cases.json --runs OUT/llm.json OUT/decide.json --out OUT/report
```

`report` writes `comparison.json`, `report.md` (the per-task recommendation table first) and `reliability_<TASK>.svg`.

## What is built and what is not run

- **Built and tested:** the deterministic case set, the perturbations, every metric, the runner with its single `Decider` door, the comparison and the recommendation table, the reliability diagrams.
- **Not run: no access.** No vendor key exists for any provider. Every path is recorded as `NOT_RUN` with its reason; nothing is faked, no recorded answer is passed off as a run.
- **Blocked on session D.** The runner reaches the model service only through `harness.decide()` and `harness.generate()`. When session D's harness PR merges, wire one `Decider` per path in `run.py` (`get_decider`) and run the bake-off once a key exists. Latency is a Lagos-region measurement only if the run says `runner_region` is a Lagos region.

## The recommendation column is a comparison, not a decision

`ELIGIBLE_FOR_FOUNDER_REVIEW` means only that in **every** language bucket `decide()` has strictly higher accuracy than the LLM path and an expected calibration error no higher than the LLM path's (PERD §19.1: adopt only where it beats the LLM and stays calibrated on `pcm`, `en-NG`, code-switched text and voice transcripts, measured separately). Anything else is `KEEP_LLM`; a path that did not run is `NOT_RUN`. **No threshold is encoded and none is changed** (AGENT-16): whether calibration is good enough in absolute terms, and whether to adopt a provider, are the founders' decisions. Model versions must be pinned before any Record-relevant use; a result without a pinned version is flagged `NOT PINNED`.

## Tests

`make bench-test`: case-set validity, determinism, coverage of every task and bucket, meaning-preservation of the perturbations (property tests), ECE and its bounds (property tests), the runner against a scripted decider, the NOT_RUN paths, the report and diagrams, and the command.
