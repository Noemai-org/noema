# Status · security-reviewer

Written only by the security-reviewer role (see `README.md`). Append dated entries, newest last. History before 31 Oct 2026 is in `status/<task-id>.md`.

## Entries

(none yet)

### 2026-11-02

- Completed: pgsodium spike extended to the T6 condition agreed with the Engineering Lead; 66 of 66 at `5904374` ([run 41427207979](https://github.com/Noemai-org/noema/actions/runs/41427207979), [noema#106](https://github.com/Noemai-org/noema/pull/106)); the workflow now runs only on its paths or by hand. T6 wording follow-up: [docs#84](https://github.com/Noemai-org/docs/pull/84).
- Completed: SEC-14 spike A1 to A7 on SeaweedFS 4.51 and pgsty/silo RELEASE.2026-10-26, both pass at `b3a77e2` ([run 41428379854](https://github.com/Noemai-org/noema/actions/runs/41428379854), [noema#110](https://github.com/Noemai-org/noema/pull/110)). The secret scanner flagged one fixed synthetic key in that branch's history, so the spike moved to branch `test/SEC-14-anchor-spike-clean` with run-time random credentials; noema#110 is superseded. Evaluation [docs#81](https://github.com/Noemai-org/docs/pull/81) updated with the results.
- Blocked: none.
- Decisions taken: none beyond scope. Architect principle recorded: the production anchor store sits in an account whose credentials the runtime operator does not hold; vendor decided after cloud documentation is verified.
- Next: SEC-13 positive test after GOV-28 is approved (noema#105 stays a draft and is never merged).
