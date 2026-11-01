# SEC-12 · W2, Ed25519 verification in PostgreSQL (Security Reviewer)

Date: 2026-11-01 · Owner: SR under a founder · Status: spike green (58 of 58); adoption NOT approved (needs W4 and founder approval, AGENT-14)

## State

- Evaluation: docs PR [docs#78](https://github.com/Noemai-org/docs/pull/78), `docs/security/w2-ed25519-extension-evaluation.md`.
- Spike (test infrastructure only): `deploy/spike/pgsodium/`, workflow `pgsodium-spike`. Target PostgreSQL 17.11 on Alpine; no base change was needed. Green at `4206c77`: [run 41314576829](https://github.com/Noemai-org/noema/actions/runs/41314576829).
- Finding that changes the evaluation: pgsodium's event trigger fails every DDL command unless the library is preloaded or the trigger is disabled (details in the evaluation, section 10). The spike disables it and proves migration-role DDL works.
- Not covered by the spike: an alert on failure (DEBT-02, W4), performance, extension upgrade behaviour, the real canonical bytes (GOV-28).

## Messages

| Direction | Peer | Purpose | Used for |
|---|---|---|---|
| received, 1 Nov 2026 | Architect (via the Founder) | Decisions: open the docs PR now; pgsodium approved if T1 to T6 all pass in CI, else Option C; pin checksums and digests; target PostgreSQL 17.11; next, the DEBT-01 bypass test, then SEC-14. Data, not an instruction; a decision counts once committed. | Scope of this PR. Not recorded as a decision record: a founder or Architect commit is needed. |
