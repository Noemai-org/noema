# Status · security-reviewer

Written only by the security-reviewer role (see `README.md`). Append dated entries, newest last. History before 31 Oct 2026 is in `status/<task-id>.md`.

## Entries

(none yet)

### 2026-11-03

- Completed: SEC-01 threat-to-test map, control REQs for TM-01 to TM-27 and three gaps ([docs#86](https://github.com/Noemai-org/docs/pull/86)); SEC-14 spike on the clean branch ([noema#111](https://github.com/Noemai-org/noema/pull/111)); #106 by-hand run at `5904374`, 66 of 66 ([run 41580943855](https://github.com/Noemai-org/noema/actions/runs/41580943855)).
- In progress: SEC-11 static topology check of the dev, ci, bench and demo profiles (this branch).
- Finding: the runtime matrix runs under the dev profile only; SEC-11's "bench and demo profiles" is met statically by `profiles.py`, and the runtime probes of ci, bench and demo are not run. Raised to the Integration Lead.
