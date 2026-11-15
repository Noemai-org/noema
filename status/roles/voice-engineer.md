# Status · voice-engineer

Written only by the voice-engineer role (see `README.md`). Append dated entries, newest last.

## Entries

### 2026-11-05 (session start, Technical Co-Founder's Claude account, Founder's GitHub identity)

**State rebuilt from live GitHub, plan v1.11 `tasks.csv`.** VOI-01 Ready (needs PLT-01, Done). VOI-18 needs VOI-01; VOI-02 and VOI-04 need VOI-01; VOI-03 needs VOI-02; VOI-05 Unassessed (a plan signed by a founder). No VOI PR existed before this session.

**In progress**
- VOI-01 on `feat/VOI-01-speech-provider-interface`: neutral types, stream-order contract, capability matrix, three refusing scaffolds. Not complete: no per-provider recorded sample exists (see Blocked).

**Blocked**
- VOI-01 `done_when` "interface tests pass against a recorded sample per provider": needs each vendor's API document and either a recorded sample or founder approval for a live call (REQ-INT-006). Nothing is invented meanwhile.
- Lint conflict: `tools/lint/pyrules.py` `harness_only_providers` allows only `harness/` to import `providers`, while the boundary docstrings let `voice` import it. The voice pipeline will take a provider through a structural Protocol it defines itself, so no import is needed. Raised in the VOI-01 PR.

- 2026-11-06: A9 (voice may import providers.speech) applied in #192 (lint plus a negative canary). VOI-02 in #193 (STACKED on #186 and #192): `voice/endpoint.py`, `voice/pipeline.py`. Open: `voice.audio_ref` has no meaning once raw audio is discarded (REQ-VOI-004); Opus decode needs an AGENT-14 dependency entry.
- VOI-18 on `feat/VOI-18-modular-pipeline-guard`, STACKED on VOI-01 (#186): `providers/speech/guard.py` refuses a speech-to-speech provider in `bench` and `demo`, and any unknown profile. Wiring it into service start-up waits for a config loader for speech providers (none exists on `main`).

- 2026-11-09: #187, #188, #192, #208, #219, #220 and docs#118 merged. VOI-02 (#193) has main merged in after #192; #200, #213, #218 follow; #228 and #230 are unstacked (parents merged).
