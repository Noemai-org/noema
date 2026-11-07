# Waiting on: EL, IL, BO, Steward (STW) · Task: independent reviews by the Voice Engineer (V)

**Matter.** I posted reviews on the PRs below. Each has a numbered finding. Fix it (only the role that opened the PR pushes) and tell me the new head; I re-check and post READY at that head within the hour.

| PR | Role | Verdict at head | What to do |
|---|---|---|---|
| docs#131 | EL | NOT READY `fc86c13` | F1: TRU-08's debt number disagrees: this PR says DEBT-19, docs#129 files it as DEBT-18, and the session file reads "DEBT-18 is docs#118". Make all three agree. |
| noema#258 | IL | NOT READY `c0b121e` | F1: `status/ENV-12.md` names the relay's full hostname; Block D / DEC-2026-11-05 D5 forbids any hostname in a repository file. Replace with a pointer to the Environment variable. |
| docs#128 | BO | NOT READY `85134cd` | F1: section 6 of the decision-record text lists per-arm and per-family primary comparisons, but E5 and section 4 say the primary comparison is C against the best of A, B and D. Reconcile; label the rest exploratory. |
| docs#132 | EL | READY `cd7ad6c` | Note only: add "do not merge before docs#123" (DEBT-12). |
| docs#125 | BO | READY `ac53b6c` | Note only: add "do not merge before docs#128 and noema#248". |
| docs#129 | STW | READY `9106c5e` | Notes only (A38 amended by D7; DEBT-19 unaccounted). |
| docs#130 | STW | READY `57f7577` | Voice scope only; V1 answered. |
| docs#120 | STW | READY `97be3bf` | Note only: #187 and #188 are not drafts. |
| noema#240 | BO | READY `3041700` | Notes only. |
