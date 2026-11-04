# Status · security-reviewer

Written only by the security-reviewer role (see `README.md`). Append dated entries, newest last. History before 31 Oct 2026 is in `status/<task-id>.md`.

## Entries

### 2026-11-01

- Completed: SEC-12 evaluation ([docs#78](https://github.com/Noemai-org/docs/pull/78), merged); pgsodium spike green, 58 of 58 at `4206c77` ([run 41314576829](https://github.com/Noemai-org/noema/actions/runs/41314576829), carried by [noema#106](https://github.com/Noemai-org/noema/pull/106)).
- In progress: SEC-13 failing-first bypass test, draft [noema#105](https://github.com/Noemai-org/noema/pull/105) (red by design, never merged); SEC-14 evaluation [docs#81](https://github.com/Noemai-org/docs/pull/81).
- Blocked: none. GOV-28 ([docs#80](https://github.com/Noemai-org/docs/pull/80)) and W4 are not mine.
- Decisions taken: the spike disables pgsodium's event trigger at bootstrap (docs evaluation, section 10).
- Escalations: pgsodium's DDL behaviour without preload (a bootstrap step W4 carries).
- Next: align the T6 pass condition with GOV-28 D13; SEC-14 spike A1 to A7 on SeaweedFS, then pgsty/silo; SEC-13 positive test after GOV-28.

### 2026-11-03

- SEC-14 spike: spike evidence for SeaweedFS 4.51 and pgsty/silo 2026-10-26 on the clean branch ([noema#111](https://github.com/Noemai-org/noema/pull/111)); the run link is in the PR. Codex found that SeaweedFS lets the writer hide an anchor with a delete marker; recorded as a check, not as a pass (see the PR).
