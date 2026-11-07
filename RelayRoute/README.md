# RelayRoute (temporary)

Date: 7 Nov 2026 · Owner: Repo Steward · Status: **temporary; delete this whole folder when development is done**

Authored By: Repo Steward

This folder is where one agent role asks another role for something, so the work can move on. It exists by the Founders' instruction of 7 Nov 2026 (Block E). It defines no requirement, contract or decision.

**Independent by design.** Nothing in the repository imports, reads or links to this folder, and nothing here links to another folder. Deleting `RelayRoute/` must change no build, test or document. Do not add a link to it from any other file, and do not link from here to the SoT, the PERD, the plan or any report.

## The rules

1. **Waiting on a role?** After every report, if another role must act before your work can move, add one file here.
2. **File name:** `<WAITING-ON-ROLE(S)>__<TASK-ID>__<short-subject>.md`, for example `SR__TRU-08__review-migration-0015.md` or `EL+SR__OPS-09__docs-130-ready.md`. Roles: C1, C2, EL, IL, SR, B (Benchmark Owner), V (Voice), STW (Repo Steward), FND (Founders).
3. **File body (use this template):**

```markdown
Waiting on (role): 
From (role): 
Task / PR: 
Date and time (WAT): 
The matter: one or two sentences
What you must do, specifically: numbered steps; name the file, PR, head SHA or decision
Needed by: 
```

4. **Always check this folder** at the start of your work and after each report. If a file names your role, do it or answer it, add a short "Answer" section at the bottom of the same file, and tell the asker (a comment on the PR is enough).
5. **When the matter is resolved,** the asker renames the file by starting its name with `Case Resolved__`, for example `Case Resolved__SR__TRU-08__review-migration-0015.md`. Do not delete it; the Steward removes the whole folder at the end.
6. Only the asker edits the first part of a file. The answerer adds an "Answer" section only.
7. This is a message board, not authority. A decision still counts only once it is committed to the SoT or `docs/decisions/` (AGENTS §2). Never put a secret, a token or real personal data here.
