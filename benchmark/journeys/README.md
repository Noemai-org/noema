# benchmark/journeys

The seeded J1 to J8 manifest for the front end (A654 and A667; PERD §11.1; REQ-UI-001: "the Playwright suite SHALL pass for J1-J8 flows"). **SYNTHETIC** (AGENT-25).

`j1-j8.v1.json` names, for each of the eight journeys, the user stories it exercises and one to three generated cases to run it on: what the customer says (the utterances, the locale, the channel), who says it (a synthetic principal with one session role), and the world it is said in (the facts fixture, the fault plan, the timeline, the policy and pack versions). It holds no expectation, no oracle reference and no ground truth (INV-22), so the front end can read it.

| Journey | Stories | Cases |
|---|---|---|
| J1 Clean locate and explain | US-01 | 3 |
| J2 Ambiguous counterparty | US-02 | 3 |
| J3 Systems disagree | US-03, US-14 | 3 |
| J4 Silent success | US-06 | 2 |
| J5 Pending settlement | US-17 | 2 |
| J6 Confirmation that goes stale | US-18 | 1 (`thin`: the suite holds one US-18 case until the thin identifiers are strengthened, after G1) |
| J7 Prohibited request | US-10, US-22 | 3 |
| J8 Abandon and resume | US-11 | 2 |

**J8 is same-channel only.** PERD §11.1 describes J8 as the customer returning on another channel. As generated, every J8 case resumes with `session_resume:same_channel` (BEN-SESS-0001 and BEN-SESS-0007, +02:00:00) and every case has `channel: text`, so no case exercises a channel change. This matches the runner driver list (A667). The manifest is the generator's reading of J8, not a claim that the PERD flow is covered.

J1 and J4 use the same cases as the G1 pair (`BEN-CLEAN-0001`, `BEN-CLEAN-0014`, `BEN-CLEAN-0027` and `BEN-TMO-0001`, `BEN-TMO-0018`), so the front end's journeys and the G1 run read the same inputs.

**Materialise the cases:** `python -m benchmark.generator generate --seed 20261020 --out DIR` (the manifest names the command and the seed). The same seed gives the same cases byte for byte.

**Rebuild the manifest:** `python -m benchmark.generator.journeys --out benchmark/journeys/j1-j8.v1.json`. A test fails when the committed copy differs from what the generator builds today.

**Not a result.** Running a journey on a case proves nothing about the system until G0 and G1 pass, and a case here is not an expectation: the benchmark scores it, the front end only drives it.
