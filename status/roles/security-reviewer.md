# Security Reviewer: status

Append dated entries. Never rewrite history.

## 2026-11-01

- Completed: SEC-12 evaluation ([docs#78](https://github.com/Noemai-org/docs/pull/78)); pgsodium spike green at `4206c77` ([run](https://github.com/Noemai-org/noema/actions/runs/41314576829)).
- In progress: SEC-13 failing-first bypass test, draft [noema#105](https://github.com/Noemai-org/noema/pull/105) (red by design); SEC-14 research.
- Blocked: none. W4 and GOV-28 are not mine.
- Decisions taken: none beyond the task's scope. The spike disables pgsodium's event trigger (recorded in the evaluation, section 10).
- Escalations: pgsodium's DDL behaviour without preload (a bootstrap step W4 must carry; an extension upgrade could re-enable the trigger).
