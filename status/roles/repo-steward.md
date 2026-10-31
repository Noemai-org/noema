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
