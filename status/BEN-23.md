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

# Session 2026-11-24 · BEN-23 · A604: SYNTHETIC boundary recordings of the `requested_remedy` gate, for C2

## Session summary
- What changed: `benchmark/compile_acceptance/synthetic_boundary.py` (the generator), `boundary-a604/` (143 files: 19 boundaries as a compile-run recording set, a findings file, a README) and `test_synthetic_boundary.py` (9 tests). Added as separate commits to the A604 labelled-set PR, as the dependent work of the same ruling (AGENTS §11).
- How: the real compile service (pipeline, harness, provider adapter, recorder) runs once per boundary with a scripted inner transport. The only thing that changes per boundary is the probability the "model" wrote for the gate question; each boundary uses the next utterance of the labelled set so the requests, which a replay keys by SHA-256, are distinct. Run at noema#932's head `4d2dbb72`; two runs were byte-identical.
- Expected outcomes are computed from the written text by exact integer and fraction arithmetic and the rule of A612, not from the implementation. Result: 12 of 19 generated boundaries agree. Seven disagree and are stated, not hidden: B14 (`9e-1` read as the float 0.9, so accepted) and B15 to B20 (a sign, NaN, infinity, null, missing, a string: the harness refuses the whole reply, and the compile service reports `ProviderUnavailable`, instead of the slot abstaining). B12 (just over one) disagrees the same way and is not committed.
- Not committed: B02, B06 and B12, because their text holds five digits in a row, which the leak rule refuses in a committed file. They are generated with `--include-long-digit-runs`.
- Risks, and how they are contained: SYNTHETIC, not live: every file has `run_id` `synthetic-boundary`; the README and the findings say "not live". The recorder's own manifest label still says "recorded live output (SYNTHETIC)"; it is read with `run_id`. The request bytes depend on #932's code and pack, so the keys change when #932 changes (regenerate).
- Tests added (A604, A612): `test_synthetic_boundary.py`. Two mutants (a changed recorded probability; a flipped expected outcome) each fail a test. Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: after noema#932 merges, regenerate the recordings (the command is in `boundary-a604/README.md`) and replace the directory; C2 replays the labelled set against the gate with them. The Architect and C2 decide B14 and B15 to B20.
- Open questions (owner): whether a malformed gate probability should abstain the slot or stop the compile as a refused reply (the Architect, C2); whether three five-digit boundary lines may be allowed in `leakscan.allow` (SR).
- Do not touch: the goldens, the held-out set (INV-22), any threshold (AGENT-16).
