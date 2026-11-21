# Session 2026-11-16 16:00 WAT · SEC-02 · the connection-test matrix, as CI ran it (IL)

## Session summary
- What changed: this file records the connection-test matrix of SEC-02 (`deploy/compose/netcheck/matrix.py`, run by `make net-matrix` in the `compose` job) and the CI run that shows it passing at a head on `main`. Nothing else is claimed.
- The run: `compose` run **43891129621**, on `main` at `34dbcfe2` (16 Nov 2026), conclusion success. Its log ends: `matrix OK: 31 forbidden paths refused, core and model-without-egress cannot reach outside, every other path as PERD §65.1 allows`. The static half runs first in the same job: `netcheck/profiles.py` on the real resolved configuration of the four profiles (dev, ci, bench, demo) and its self-test, `61 mutations; every one is caught`.
- What the matrix checks, by its own numbered headings in the log:
  - **1** network membership from `docker compose config`;
  - **2** reachability, probe to target (open only where a network is shared);
  - **TEN-01** no runtime container can reach the oracle (network and credential);
  - **3** forbidden paths from inside the real model container (the model cannot reach the core database, Temporal or the enterprise adapters);
  - **4** database roles;
  - **5** egress: can each probe reach an address outside the stack (the core and the model without `provider_egress` cannot; the model with it and the UI may);
  - **6** mock-case: the truth endpoint opens for the bench credential and refuses every runtime identity;
  - **7** the evil provider (bench profile only);
  - **8** the webhook relay: a pull without the pull token is refused in the running stack;
  - **9** each run profile started on its own and probed from the core, the model and the UI (ci, bench, demo).
- Why: the SEC-02 row of `tasks.csv` says "Connection-test matrix passes in CI" and names the isolation it must show (the runtime cannot reach the oracle; the model service cannot reach Postgres, Temporal or Tushe; the core cannot reach providers).
- Risks, and how they are contained: this is a record of one CI run, not an acceptance. SEC-02's owner is the Security Reviewer with an agent; the task's Done is theirs and the Steward's to mark. The matrix runs against the Compose stack, not a shared environment: the host firewall rules of REQ-OPS-001 are not shown here.
- Tests added: none (a record). Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the Security Reviewer reads this against the run and says whether it meets SEC-02's "Done when"; the Steward then marks the row.
- Open questions (owner): whether the Tushe half ("the model service cannot reach Tushe") needs a probe of its own: the model has no route to `tushe_net`, which step 2 shows, but there is no probe aimed at a Tushe address (Security Reviewer).
- Files to read first: `deploy/compose/netcheck/matrix.py`, `deploy/compose/README.md` ("Routes out").
- Do not touch: the matrix's expected table without a decision (PERD §65.1).

# Session 2026-11-20 · SEC-02 (Temporal TLS, noema#698)

## Session summary
- What changed: Codex's P1 on noema#698. In the pinned template (docker/config_template.yaml, release 1.29.7) `TEMPORAL_TLS_SERVER_CA_CERT` is both the root of the frontend client and the root of the internode client, so setting it made the services dial each other over TLS while the internode listeners had no certificate. The bench Temporal now also sets `TEMPORAL_TLS_SERVER_CERT`, `TEMPORAL_TLS_SERVER_KEY` and `TEMPORAL_TLS_INTERNODE_SERVER_NAME` (the same `temporal` certificate, name `temporal`); netcheck requires all ten settings, with three new mutations. 91 of 91 mutations are caught.
- Wording (Security Reviewer's note 1): this is **server-side TLS with no client authentication. It is not mTLS for Temporal.** Until DEBT-46 closes, only the network keeps another container from starting a workflow; SEC-02's matrix (the model is refused a route to Temporal) stays in the matrix.
- Not verified here: the stack itself (no Docker daemon in this session). The compose job on the PR is the first run with internode TLS; if the services cannot reach each other, the fix is one push.
- Open: DEBT-46 needs an owner and a date, and the Architect's ruling on the internal-frontend split (Security Reviewer's note 2).
