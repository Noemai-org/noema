# Session 2026-11-20 · VOI-05 · the private raw-audio store (Integration Lead)

## Session summary
- What changed: `deploy/compose/audio-store/` (A451): `store.sh` (`init`, `check`, `purge`), the systemd purge service and timer for the bench host, a README, `test_store.sh` and `test_never_leaves_the_host.py`, both run in `compose-scripts`.
- Why: A451 (IL creates the private store on the bench host: a directory, mode 0700, a retention timer, never uploaded, never in the repo or CI artifacts; SR reviews), REQ-VOI-004, A377 to A379.
- Risks, and how they are contained: raw audio is never handled by this code (it makes a directory and deletes old files). The store and retention are host settings with no default; the script refuses without them and refuses a store that is not mode 0700, owned by the caller, not a link, not inside the checkout. Purge prints counts, never names, never follows a link.
- Tests added (REQ-VOI-004): see the README. Seen failing first: the guard test failed with no files; `test_store.sh` failed with no `store.sh`.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: Voice or the Founders state the retention number (the VOI-05 plan row says only "a retention timer"; the Integrator account's own retention is zero days, A379) and who installs the units; a Founder creates the `noema-voice` account and `/etc/noema/audio-store.env` (mode 0600).
- Open questions (Founders): the retention number; whether bystanders may be recorded (A452, a recommendation awaiting the Founder).
- Not verified: `test_store.sh` beyond its refusal cases (this host is Windows: modes are not real here); CI on Linux is the proof.
- Do not touch: no audio, no path of a store, no number in the repository.

# Session 2026-11-20 (later) · VOI-05 · Voice's and SR's reads of noema#819 (Integration Lead)
- Added `store.sh remove <speaker_ref>` (a withdrawn speaker's folder; counts only) and `store.sh scan` (objects, objects without a speaker_ref, links, objects open to group or other; counts only; exit 1 if any bad). The README says the retention runs from ANALYSIS: the voice runner sets each file's modification time to the analysis date, so 90 days run from analysis (Voice's correction: per file, not per folder), and that encryption at rest is the host's (a Founder's step). The plan's value is 90 days, set on the host.
- SR's notes: no fixed `ReadWritePaths` in the unit (the path is the host's setting), the unit hardened (no network, devices or capabilities, system-call filter, umask 0077), GNU `stat` required and checked, and files inside the store must be 0600 or the scan fails. Tests first for each.
- Not verified: `test_store.sh` past its refusal cases (Windows host); the units have not been run.
