# Status · repo-steward

Written only by the repo-steward role (see `README.md`). Append dated entries, newest last. History before 31 Oct 2026 is in `status/<task-id>.md`.

## Entries

### 2026-10-31

**Completed**
- Task 1, the `tasks.csv` assessment of all 226 rows: [docs#73](https://github.com/Noemai-org/docs/pull/73) (draft, awaiting the Technical Co-Founder). Result: 31 Done, 1 In review, 40 In progress, 15 Ready, 124 Backlog, 15 Unassessed. Evidence is main at noema `448fdca`, where all 13 push workflows are green.
- Task 2, per-role status files (this directory) and the CI edit-trigger change (the pull request that carries this file).

**In progress**
- Morning PR report: below.

**Blocked**
- Nothing.

**Decisions taken**
- Rows whose truth is outside the repositories or in held-out custody are left `Unassessed`, not guessed (list in docs#73).
- `Verified` is not used: Done requires merged code and tests that meet `done_when`.

**Escalations**
1. Status and ID mismatches for the Architect: `status/UND-02.md` covers the harness and pipeline, but plan row UND-02 is the Input Gateway; `status/GOV-22.md` covers the drift guard, but plan row GOV-22 is branch protection. Option A: rename the status files to the work they describe and re-key the plan rows. Option B: leave the files and add a redirect line to each. Recommend A for UND-02 and B for GOV-22 (renames break links in vendored text). Question: which?
2. Branch protection is still off (the GitHub plan, per `pr-base-guard.yml`). Until then `pr-base-guard` and `main-merge-audit` are the only enforcement.

**Open PRs at 2026-10-31 (noema)**
- [noema#90](https://github.com/Noemai-org/noema/pull/90) (23 files) and [noema#94](https://github.com/Noemai-org/noema/pull/94) (4 files): EXE-04, both HELD for DEBT-01 (`status/EXE-04.md`).
  - Overlap: #94's four files are all also in #90 (`services/core/migrations/0002_grant_spent_void.sql`, `services/core/migrations/README.md`, `services/core/platform/db/grant_void_test.go`, `status/ENV-03.md`). The titles suggest #94 is the migration and #90 the controller change built on it; I have not verified the intended merge order, so the owner confirms it.
  - Behind main: both heads predate #98 (status-only, EXE-04 HELD). `status/ENV-03.md` and `status/EXE-04.md` will conflict when `main` is merged in. Owner (Core Engineer 1 / Session A) resolves after the hold lifts; the instruction to not merge `main` into #90 stands. I did nothing to either branch.
- docs has no open PRs. This session's docs PR is [docs#73](https://github.com/Noemai-org/docs/pull/73).

**Red runs on main:** none. All 13 push workflows green at `448fdca`; scheduled docs-drift green on 31 Oct.

**Next task**
- Daily PR overlap report, then re-point watch after the next docs merge.

### 2026-11-03

**Correction to the 2026-10-31 entry (history above is left as written).** The 226-row assessment in docs#73 was superseded by [docs#77](https://github.com/Noemai-org/docs/pull/77) (261 rows, merged), which also filled the architecture map's Status column (OPS-08) and added DEBT-03. The mock-case red run named above is cleared: [mock-case is green on `main` at `b2047e4`](https://github.com/Noemai-org/noema/actions/runs/41325126389) after noema#103.

**Completed**
- docs#75 (DEC-2026-10-31 change package), docs#76 (`plan_index` accepts TBC under "To be dated at the re-baseline"), docs#79 (SEC-12 and EDG-01 wording), docs#77 (OPS-09 and OPS-08), noema#99, #100, #101 (OPS-07 re-point): all merged.
- [docs#85](https://github.com/Noemai-org/docs/pull/85): refresh of 12 statuses after the merges of 1 to 3 Nov (draft until its checks are green).

**In progress**
- Reading the 3 Nov open PRs for overlap (done in docs#85's body). Vendored copies: `repoint_docs.py --check --ignore-commit` against docs `8e53696` passes, so no re-point is owed; docs-drift stays green.

**Blocked:** nothing.

**Decisions taken:** ENV-08 is Done by its done-when now that mock-case is green on `main`; the Compose-path note from the Integration Lead is recorded, not held against the row (docs#85 body, for the Architect to veto).

**Escalations:** the founder's standing orders of 3 Nov (demo Sat 12 Dec 14:00 WAT, no scope cut, new gate dates) disagree with committed documents (plan v1.10 §2 cut line and the 13 Nov demo, DEC-2026-10-16 E3). AGENTS §2 item 3: until they are committed I follow the committed documents and raise the conflict. Recommendation: the Architect commits them as a decision record with plan v1.11 and the gate records, folding in GOV-30 (docs#83). I change no date, gate record or plan text on my own.

**Open PRs at 2026-11-03:** noema #111, #109, #108, #107, #106, #105, #104 (held), #102, #94 and #90 (held); docs #84, #83, #80.

**Next tasks**
1. Mark docs#85 ready for review when its checks are green.
2. Morning overlap and red-run report, and a re-point the same day as any docs merge that touches vendored text.
3. Refresh `tasks.csv` and the map as each open PR merges.
