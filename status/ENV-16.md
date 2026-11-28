# ENV-16 · the fixture-loader workflow (IL)

State: the governance half is built; the load is NOT. Task row: plan v1.11 (DEC-2026-11-04 R12 and R17). Needs ENV-05 (merged).

## Session 2026-11-26 · ENV-16

### Built (from what the documents state)
- `.github/workflows/tushe-fixture-load.yml` (R17 approves this one workflow, a new file under `.github/workflows`): `workflow_dispatch` only; the job runs only on `refs/heads/main`; it sits in the `tushe-fixture` Environment, so it waits for the Founder's review (R12 (l): main-only, Founder as required reviewer); read-only token; the checkout keeps no credential; the inputs (scenario key, seed) are checked on the whole string; each run is recorded in the step summary (commit, run, scenario key, seed, who dispatched it, never a credential).
- `tools/ci/test_tushe_fixture_workflow.py` (red first: the workflow did not exist), run by `safeguards`: by hand from main only, the Environment, no `secrets` or `vars` reference in the file, read-only permissions, validation and the record, the load step exits 3, and no runtime Compose file names the Environment or a fixture setting.

### Not built, and why (what the documents do not state)
1. **The endpoint that loads fixtures.** Specification v1.0 lists seed/reset/scenario as control-plane capabilities (section 7 and the deliverables table) without a route; delta v1.2 defines "the same run" (scenario key, version, definition checksum, seed) without one; delta v1.3 lists the partner routes (API keys, webhooks, events, audit events, transitions) and none loads data. The load step says NOT RUN and exits 3, so the workflow never reports a pass.
2. **The credential and its scope.** Delta v1.3 mentions "the runtime and fixture keys" as separate from rotation, but gives no name, scope list or how a fixture key is obtained. The secret names for this Environment are not chosen here and are not in `.env.example`; the guard test refuses any `secrets` reference until they are.
3. **The request and response bodies**: how a scenario definition is sent, what the checksum is computed over, what an accepted load returns, and what makes a repeated load idempotent.
4. **The acceptance** ("two runs of the same seeded fixture set give identical Tushe state"): it needs 1 to 3 and a Tushe environment to run against.
5. **Whether the Environment exists** and its branch rule and reviewer: a GitHub setting the Founders own; nothing in the repository shows it.
- Questions for Tushe, to be sent by the Founders with the other eight (noema#690): the route and body that load a scenario and its seed; the fixture key's name, scope and lifetime; the response of a load; idempotency of a repeated load.

## Pre-work for the next agent
- Exact next step: when a Tushe document states the load endpoint and the fixture key, replace the NOT RUN step with the call (the guard test and `.env.example` change in the same PR; SR reads, the Founder approves).
- Do not touch: the frozen Tushe specification.
