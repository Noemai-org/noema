# Status · engineering-lead

Written only by the engineering-lead role (see `README.md`). Append dated entries, newest last. History before 31 Oct 2026 is in `status/<task-id>.md`.

## Entries

### 2026-11-01 14:52 WAT

- **Completed:** none (nothing merged).
- **In progress:** PCK-04, draft PR on `claude/new-session-s2li0v`. Contract `action-graph.v2` with `roots`, the packcheck graph-shape rules and five bad-pack fixtures are built, and their tests pass locally. The fintech pack fails packcheck for two nodes until Core Engineer 2 adds the beneficiary and account descriptors (E2). See `status/PCK-04.md`.
- **Blocked:** PCK-04 green, on E2 (Core Engineer 2, since 1 Nov 2026). REQ-AG-003 evidence, on the planner (Core Engineer 1).
- **Decisions taken:** none of my own. Applied the Architect's decisions of 1 Nov 2026 (E1, E2, the order of work, the branch).
- **Escalations:** none new.
- **Next task:** the D32 re-baseline proposal once docs#77 merges (within two working days); W1 (GOV-28) draft in `docs` with pgsodium, spike checks T1 to T6 and the Option C fallback, due Wed 4 Nov; then the Trust parts of the G1 slice (US-01, US-06).
- **Trust map (31 Oct 2026, `main` 448fdca):** TRU-01, TRU-02, TRU-04, TRU-05, TRU-07 and TRU-12 partial; TRU-03, TRU-06, TRU-08, TRU-09, TRU-10 and TRU-11 absent. Details in the first report of the session.

### 2026-11-01 evening WAT

- **Completed:** none merged.
- **In progress:**
  - PCK-04, noema#104 (head `7dc2eb4`): CI red only for E2, as expected ([contracts](https://github.com/Noemai-org/noema/actions/runs/41313689120/job/124869179798)).
  - W1 (GOV-28), docs#80: updated for the W2 spike (Alpine kept, the event trigger disabled at bootstrap, the T6 condition); approval not requested, the Architect is reviewing.
  - GOV-30, docs#83: the re-baseline proposal.
  - TRU-02, this PR: the T1 to T15 runner.
- **Blocked:** PCK-04 green, on revised E2 (Core Engineer 2's descriptor PR merges first).
- **Decisions taken:** the T10 row read as ESCALATE only (safer), raised in the TRU-02 PR.
- **Escalations:** the four GOV-30 decisions (docs#83, §4).
- **Next task:** TRU-05, then TRU-04 (the G1 slice).

### 2026-11-03 evening WAT (Architect's reply of 3 Nov)

- **Request to the Repo Steward (task row, for `tasks.csv` and plan v1.11; I do not edit either):** Codex's basis-binding follow-up on noema#118 ("the basis remains caller-supplied; it is not proof the rules or evidence were evaluated until the decision path and the Decision Record bind it").

  | Field | Value |
  |---|---|
  | task_id | TRU-13 |
  | week / date | 1 / 2026-11-06 |
  | workstream | TRU |
  | task | Basis binding: the Decision's basis (policy rule versions, evidence snapshot, freshness) is built by the gate steps from what T9 and T11 actually read and evaluated, never supplied by the caller; the Decision Record refuses a decision whose basis differs from the evaluated set |
  | owner | C1+AG (EL) |
  | req_ids | REQ-TRU-001;REQ-TRU-004;REQ-TRU-007 |
  | needs | TRU-02;TRU-03;TRU-05 |
  | done_when | A test that changes the basis between evaluation and recording is refused; 0 ALLOW or CONFIRM whose basis is not the evaluated set (property test) |
  | current_status | Backlog |

  The first half is already in noema#133 (TRU-02: `Evaluate` refuses an ALLOW or CONFIRM with an incomplete basis). Binding to what was evaluated needs TRU-05's Decision Record.
- **Done today:** noema#121 TRU-03 (CONFIRM alone is default deny), docs#87 (supersession clause), docs#88 (D5 removed), noema#139 (init.sh).
- **Asked of the Architect:** a distinct reason code needs a PERD §23.3 change and an AGENT-15 contract bump (closed enum); the fintech pack has no PERMIT for `log_complaint_reference`, so a complaint is now denied.
