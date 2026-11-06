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
