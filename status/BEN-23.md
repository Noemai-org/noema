# Status · BEN-23 (Benchmark Owner agent with the Voice Engineer) · Intron on the voice subset

Updated 2026-11-16. Task row: BEN-23 (plan; rewritten by the A179 change request to "Intron on the voice subset: per-locale key-slot accuracy, latency and cost, paced for Intron's documented rate limits", ruling A182). Gate: G5 (voice); G5 thresholds do not change.

# Session 2026-11-16 · BEN-23 · the plan for the voice-subset run, three paced sessions, synthetic audio only

## Session summary
- What changed: `benchmark/runner/PLAN-intron-voice-subset-run.md` (a plan only) and this file.
- Why: the Founder's instruction of 16 Nov 2026: plan the voice-subset run against Intron with pacing for three sessions, synthetic audio only.
- Risks, and how they are contained: documentation only. Nothing is built or sent. A179 takes effect only with the SoT edit (v1.12), so the plan says nothing runs on Intron before it, and it invents no rate limit (OD-P-90: Intron's written answer supplies it). Real audio is made impossible to send by a provenance check in the sender, not by a promise (A180).
- Tests added: none (a plan; the checks it will start with are in its sections 4 and 5). Regression cases added: none. Debt entries: none. Conflicts raised: none (A179's version number is the Steward's open question, not mine).

## Pre-work for the next agent
- Exact next step: when the SoT edit that adopts A179 is approved and Intron's written answer on limits arrives, the Voice Engineer builds the adapter (VOI-01 as rewritten); BO then builds the audio-set generator and the scorer (key slots read from the utterance's own source text, no oracle) with tests first.
- Open questions (owner): the six in section 8 of the plan (limits, locales with TTS, per-token confidence, the meaning of "three sessions", clean-only if VOI-17 is late, the cost ceiling): the Founder, Voice, Intron.
- Files to read first: `benchmark/runner/PLAN-intron-voice-subset-run.md`, `DEC-2026-11-13-voice-provider-intron` in the `docs` repository.
- Do not touch: no Intron key value in any file; no real recording on this path.

# Session 2026-11-19 · BEN-23 · A377 to A379: the DPA, terms, consent and counsel preconditions are removed from the plan

## Session summary
- What changed: `benchmark/runner/PLAN-intron-voice-subset-run.md` only. The A180 bullet is replaced by A377 to A379 (docs#211, interim until a non-author Founder merges it): no DPA, written-terms, consent or counsel precondition; raw audio stays in the bench Environment's private store; synthetic audio stays labelled and this run still uses synthetic audio only (the section 5 check is kept as the label, not as a wait for a term); no spend for a custom voice (also added to precondition 5 of section 6).
- Not changed: the other preconditions of section 6 (the SoT edit adopting A179, the qualified model ids under UND-13, the adapter, Intron's capacity answer, the cost ceiling and the key, the leak scan), the pacing, and the three sessions.
- Risks, and how they are contained: documentation only; the plan cites a ruling that is not yet merged, and says so.
- Tests: none (a plan). Regression cases: none. Debt: none. Conflicts: none.

## Pre-work for the next agent
- Exact next step: when docs#211 merges, drop the word "interim" from the A377 to A379 bullet.
- Do not touch: section 5's check that only audio the run itself generated can be sent.

# Session 2026-11-20 · BEN-23 · Codex's P1 on noema#795: A180 governs until docs#211 merges

## Session summary
- P1 (Codex, at b75bf347): the plan said A377 to A379 replace A180's written-terms precondition, but docs#211 is open, so the rulings are relayed and not committed (AGENTS section 2, item 3). Fixed: the A180 bullet is restored word for word as the governing rule; A377 to A379 are stated as **proposed, not in force**, with what they would change; the "no spend on a custom voice" in precondition 5 of section 6 is conditional on A379 being committed. The sentence in section 5 about a future TEAM_RECORDING label stays (it only applies if a later run adds recordings).
- The run itself is unchanged and synthetic-only, so it is consistent with A180 and with A377 to A379 alike.
- Tests: none (a plan). Regression cases: none. Debt: none. Conflicts: none.

## Pre-work for the next agent
- Exact next step: when docs#211 merges, replace the A180 bullet with the A377 to A379 one (a follow-up edit).

# Session 2026-11-20 (second) · BEN-23 · docs#211 merged: A377 to A379 are the rule

## Session summary
- docs#211 merged on 20 Nov 2026 (the rulings are committed). The plan is brought in line, as Codex's P1 asked for: the A180 bullet and the "proposed, not in force" bullet are replaced by one A377 to A379 bullet stating them as the rule (no DPA, written-terms, consent or counsel precondition; raw audio in the bench Environment's private store; synthetic audio labelled and this run synthetic-only; no spend for a custom voice), and precondition 5 of section 6 is unconditional again. The TEAM_RECORDING sentence of section 5 stays.
- Tests: none (a plan). Regression cases: none. Debt: none. Conflicts: none.

## Pre-work for the next agent
- Exact next step: none for this plan; the run waits on the SoT edit adopting A179, the qualified model ids, the adapter, Intron's capacity answer and the cost ceiling and key (section 6).

# Session 2026-11-21 · BEN-23 · A503, A476 and row 12

## Session summary
- A503: row 2's US-06 expected events start with the ESCALATE disposition decision, then the action decisions that reference it (`disposition_decision_id`), all before the grant.
- A476: `after_replay` is a re-read; row 2 requires it to equal the read after the recovery; the package text says re-read, not replay, and INV-21 is not claimed.
- Row 12 is computed from a stamped artifact file list (`--artifact-files`) and the leak scan.
- Tests: 17 new (red first); 14 mutations, each caught.

## Pre-work for the next agent
- IL writes the stamped artifact file list in the workflow; C1 confirms `disposition_decision_id` in the reconstruction facts.

# Session 2026-11-23 · BEN-23 · A503 disposition-first rule restored after #887

## Session summary
- What changed: row 2's US-06 check again requires the ESCALATE disposition decision first (with an id), then at least one action decision that carries `disposition_decision_id` equal to it, all before the grant (A503, PERD 23.1, REQ-TRU-010). It was taken out of #875 at Codex's P2 because the audit route did not project the field; #887 merged and the route's DECISION facts now carry `decision_id` and `disposition_decision_id` (`services/core/record/reconstruct/project.go`, `reconstruction.v3`). PR link: the follow-up PR of this branch.
- Why: the Record must show the disposition and the action decisions that reference it, not only that a decision was made.
- Risks, and how they are contained: the rule reads the first DECISION item as the disposition, so it depends on the projection keeping event order; a reconstruction built before #887 fails row 2 (fails closed). Not yet checked against a real reconstruction of the stack run.
- Tests added (REQ-TRU-010, A503): `DispositionOrder` (seven cases) and the US-06 fixtures now carry the disposition and the action decision. Two mutants of the guard each failed a test.
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: read CI at the PR head, post READY FOR CODEX; when the first real stack run exists, check that its reconstruction lists the disposition first.
- Open questions (owner): whether the real projection orders items by event sequence (EL, C1).
- Files to read first: `benchmark/suites/g1_package.py` (`_us06_problem`), `services/core/record/reconstruct/project.go`.
- Do not touch: `benchmark-heldout`, the oracle.

# Session 2026-11-24 · BEN-23 · the row-12 fixture follows A586

## Session summary
- What changed: `FILES_OK` in `benchmark/suites/test_g1_package.py` no longer lists `leak-scan.txt` or `G1-EVIDENCE-PACKAGE.md` (A586: the scan output, the inventory and the file list are the second artifact, so the main artifact is exactly the scanned files; the package is rendered after the run). The leak-scan fixtures say 10 files to match. New test `test_the_main_artifact_never_lists_the_scan_outputs_a586`. Test-only: no change to `g1_package.py`.
- Why: the fixture contradicted the ruling. The renderer already compares the upload list with the scanner's inventory as exact sets, so a real run would have failed row 12 on the old list.
- Risks, and how they are contained: none for the product; the renderer is unchanged. Putting `leak-scan.txt` back in the fixture fails five tests (mutation check).
- Tests added (A586, A565): one. Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: none for this change; row 12 on a real run waits for IL's #924 (the second artifact and the stack workflow arguments).
- Do not touch: `benchmark-heldout`, the oracle.

# Session 2026-11-24 · BEN-23 · A652: the seeded J1 to J8 manifest for the front end

## Session summary
- What changed: `benchmark/generator/journeys.py` (the builder), `benchmark/journeys/j1-j8.v1.json` and its README, `benchmark/generator/test_journeys.py` (10 tests).
- Why: A652 asks BO to supply the seeded J1 to J8 manifest to FE (REQ-UI-001, PERD §11.1). It names, per journey, the user stories (the PERD's table) and one to three generated cases with the principal, utterances, channel, fixture, fault plan, timeline and versions, and no expectation, oracle reference or ground truth (INV-22). It is reproducible by the generator's seed; a stale committed copy fails a test.
- J1 and J4 use the G1 pair's inputs, so the front end and the G1 run read the same cases. **J6 is thin: one case**, because the suite holds exactly one US-18 case; strengthening it waits until after G1 (changing the generator moves the manifest digest the G1 inputs record).
- Risks, and how they are contained: SYNTHETIC; the manifest holds the generator's seed as an eight-digit number, which the digit-run rule flags if the file is ever scanned (the same seed is already in committed G1 inputs); a test strips it before it checks the rest.
- Tests added (REQ-BEN-001, REQ-UI-001): 10. Seen failing first (import error); a mutant that adds `expected` to a case fails 2. Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: FE reads `benchmark/journeys/j1-j8.v1.json` and materialises the cases with the generator.
- Open questions (owner): whether FE needs a second case for J6 before G1 (FE, the Architect).
- Do not touch: the generator's cases before G1.

# Session 2026-11-24 · BEN-23 · A604: the offline labelled set for `requested_remedy`

## Session summary
- What changed: `benchmark/compile_acceptance/remedy_labelled.v1.json` (28 synthetic phrasings: 14 en-NG and 14 Pidgin, 14 explicit asks and 14 not, 7 in every cell; three marked borderline with the reason), a strict reader `remedy_labelled.py` (`load`, `parse`), and `test_remedy_labelled.py`.
- Why: A604. Live run 7 asserted a remedy on two status questions (G-A, G-B) and missed it on an explicit resend (G-E); the compiler's fix needs a set it was not written against.
- Label rule: explicit_ask is true iff the customer asks the bank to do something for them (resend, refund, reverse, escalate, cancel, send again). Questions about where the money is, whether it arrived, why it is pending, whether to send again, or a request to be told something are false. `asks_for` is a human-readable kind, not a capability id.
- Kept out of the goldens and the held-out set: the test fails if any text equals or contains a golden or a G1 input, or uses a golden or G1 counterparty name. The set carries no oracle and no capability id.
- Risks, and how they are contained: the labels are one reader's judgement. The three borderline items are named so a result can be reported with and without them. The set is small (28): a rate from it is a smoke measure, not evidence for any gate.
- Tests added (REQ-INT-003, A604): `test_remedy_labelled.py` (shape, balance, uniqueness, no digit run of five or more, no overlap with goldens or G1 inputs, a closed reader). Mutation checks: a golden text inserted, and the Pidgin asks moved to en-NG, each fail tests.
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: C2 uses `load(Path("benchmark/compile_acceptance/remedy_labelled.v1.json"))` to score the compiler's gate offline with the replay provider, and reports the false-assertion and the miss counts separately, with and without the borderline items.
- Do not touch: the goldens, the held-out set (INV-22), or any threshold (AGENT-16).
- Split (A652, 24 Nov): this PR is the labelled set only. The SYNTHETIC boundary recordings and their generator were taken out (a revert commit, no history rewritten) and go in their own PR, regenerated on `main` after noema#932 merges. The generator work is kept on the branch `feat/BEN-23-boundary-recordings-a604` (commit `b2aee73a`).

# Session 2026-11-26 · BEN-23 · A604, A649, A682: SYNTHETIC boundary recordings of the requested_remedy gate, for C2

## Session summary
- What changed: `benchmark/compile_acceptance/synthetic_boundary.py` and its test (from the split-out branch), `boundary-a604/fixture/` (140 reduced recordings plus the manifest, flat, `fixture.synthetic: true`, made with `reduce_run` at `d2f83578`), `boundary-a604/boundary-findings.json` (outside the fixture directory) and its README.
- Why: A604 and A667 asked for the recordings after #932, #936, #931 and #938 merged; A682 asked to regenerate them on main, matching A649 (the exponent forms are malformed and the slot abstains).
- Risks, and how they are contained: SYNTHETIC scripted replies, never live model output (A261). The leak rule: B02, B06 and B12 hold five or more digits in a row, so they are generated inside a test, never committed (the scan is clean over 143 files, no allowlisting). The generating test needs the model service's dependencies and is skipped, by name, in a job that has only `benchmark/requirements.lock`: CI would need `services/model/requirements.lock` installed for that one test (a `.github` change, SR).
- Tests added (A604, A612, A649): the generator's 9 plus 2 new ones (the flat fixture directory with `fixture.synthetic` in every file; the three long-digit boundaries generated in a temporary directory, 22 boundaries and 0 disagreements).
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: C2 points the replay provider at `benchmark/compile_acceptance/boundary-a604/fixture` and scores the gate offline with `remedy_labelled.v1.json`.
- Do not touch: the goldens, the held-out set (INV-22), the threshold (AGENT-16).

# Session 2026-11-26 · BEN-23 · A733: the boundary generating test runs in the model job

## Session summary
- What changed: `.github/workflows/model.yml` runs `benchmark.compile_acceptance.test_synthetic_boundary` with the model service's dependencies and `NOEMA_REQUIRE_MODEL_DEPS=1` (it also runs when `benchmark/compile_acceptance/` or `benchmark/requirements.lock` change); the test fails, not skips, when that variable is set and the dependencies are missing.
- Evidence: in a clean venv with only `benchmark/requirements.lock` the test skips; with the variable set it fails; in a fresh venv with `services/model/requirements-dev.lock` and `benchmark/requirements.lock` it passes (11 tests).
- Risks: a `.github` change, so SR reads it (Tier 1 path).
- Tests, regression cases, debt, conflicts: none added.

## Pre-work for the next agent
- Exact next step: open the PR after the recordings PR merges (it is stacked on that branch).

# Session 2026-11-27 · BEN-23 · Codex on #1010: replay serves 14 of the 19 committed boundaries, not the five malformed ones

## Session summary
- What changed: README (what replay serves and what it does not; the stale line about `leakscan.allow` removed), and two tests in `test_synthetic_boundary.py` (class `Replay`).
- Why: Codex's P2 on #1010 is right. B16 to B20 are recordings of a reply the harness refuses as a whole (`usable_as_fixture: false`: rec-0094 and rec-0101 `NOT_JSON`; rec-0108, rec-0115 and rec-0122 `SCHEMA_MISMATCH`), so `ReplayStore.lookup` raises `EVIDENCE_ONLY` for them by design. The findings were computed through the scripted inner transport, not through replay.
- Evidence: the new test looks every gate recording up through `ReplayStore` by its request hash: 14 are served, the five are refused `EVIDENCE_ONLY`, and nothing else in the directory is evidence-only. The supported offline path for the five is the generator (`synthetic_boundary`), which runs the real pipeline against a scripted transport; its test checks all 22 boundaries.
- Risks: none to the product. Tests added: 2 (the README test was seen failing first). Regression cases, debt, conflicts: none.

## Pre-work for the next agent
- Exact next step: C2 scores the five malformed boundaries through the generator, and the other fourteen through replay.
