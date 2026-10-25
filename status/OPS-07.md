# Status · OPS-07 (Session A) · vendored copies, REQ-OPS-005

Updated 2026-10-24. Owner: Session A. Related tasks: GOV-22 (drift guard), CON-01 (contracts).

## State

- docs#57 merged: PERD v1.15 (REQ-OPS-005; §39.3 `TENANT_SCOPE_MISMATCH`; REQ-AMB-002 no time window is `INPUT_INVALID`), plan v1.9 (task OPS-07), DEC-2026-10-23, OD-P-70 (waiting for the Founder's "yes").
- noema#56 merged: five vendored copies re-pointed to docs `67a60ec`; `tools/vendor/test_repoint_bodies.py`. noema#61 merged: `status/` and the READMEs.
- docs#58 merged (24 Oct): `.gitleaksignore` with one fingerprint (SoT line 2148 example key).
- 25 Oct: docs#59 (DEC-2026-10-25: ENV-03 starts, Go Postgres driver and Temporal SDK approved, mock-case secret names, CODEOWNERS for `/.gitleaksignore`) and noema#67 (ENV-03 first slice: `adapters.Case`, `CaseClient`) are open for the non-author founder. This PR adds the two approved secret names to `.env.example` and renames `EXE-01.md` to `EXE-02.md`.
- main at `48f93f7` was red for a day with jobs that never started; all 12 workflows were green after one re-run each (24 Oct). The cause was not seen.

## Open

- The `contracts/fixtures/README.md` line about `secret_hex` waits for the Founder's written confirmation on a pull request that it is test-only (nothing is added until then).
- Tushe-dependent work is paused until the Founder confirms (DEC-2026-10-25 D8).
- OD-P-70 (certificates, pack mount) needs the Founder's "yes".
- The abandoned branch `sa/env-03-case-adapter` (one gitleaks finding in a test placeholder, never in a PR) could not be deleted by me; a founder can.
- `docs/sessions/` holds five session files owned by other sessions; moving them to `status/` needs the owners and an AGENTS §9 change (two of them are the same task twice, so the names would collide).

## Messages

The messages for other tasks are in their own files: `status/CON-01.md` (17:04Z and 17:07Z, 21 Oct), `status/EXE-02.md` (18:35Z, 21 Oct) and `status/PCK-01.md` (14:23Z, 22 Oct).

### 2026-10-21 20:11Z sent · Session A → Session B
- Purpose: on the Founder's instruction: asks Session B to quote REQ-ENV-005 for "IMPOSSIBLE_STATE belongs to the proxy" in noema#51.
- Receiver used it for: not recorded.
- Evidence: noema#51; DEC-2026-10-22.

### 2026-10-22 14:47Z sent · Session A → Session D
- Purpose: verified Session D's two points against PERD v1.14 and the contract (TIME_VAGUE in §20.3 and the no-window result).
- Receiver used it for: Session D later changed its pipeline to the Founder's ruling (see 2026-10-23 10:20Z).

### 2026-10-23 10:13Z sent · Session A → Session D
- Purpose: `INPUT_INVALID` exists in `contracts/schemas/errors.v1.json`, non-retryable.
- Receiver used it for: not recorded.

### 2026-10-23 10:19Z sent · Session A → Session D
- Purpose: same confirmation, with `TENANT_SCOPE_MISMATCH` present and `SCOPE_VIOLATION` absent; docs#57 changes §39.3.
- Receiver used it for: not recorded.

### 2026-10-23 10:20Z accepted · Session D → Session A
- Purpose: per the founder, a request with no time window returns `INPUT_INVALID` (`REJECTED_AT_GATE`); `TIME_VAGUE` is no longer raised; `BENCH_OUT` cost records are not yet wired; PR noema#60.
- Used it for: reported it to the Founder; recorded the ruling in PERD v1.15 only after the Founder gave it in this session (docs#57), not on the peer's word.
- Evidence: docs#57 (REQ-AMB-002, DEC-2026-10-23 D3).

### 2026-10-23 10:38Z sent · Session A → Session D
- Purpose: the docs now match Session D's code (docs#57 at `9861aa6`).
- Receiver used it for: not recorded.

### Earlier incoming messages
Session C (21 Oct 17:06Z) and Session D (22 Oct 11:16Z) sent "Needs session A" lists. The bodies were not recoverable from the session record, so they are not summarised here.
