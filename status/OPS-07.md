# Status · OPS-07 (Session A) · vendored copies, REQ-OPS-005

Updated 2026-10-24. Owner: Session A. Related tasks: GOV-22 (drift guard), CON-01 (contracts).

## State

- docs#57 merged: PERD v1.15 (REQ-OPS-005; §39.3 `TENANT_SCOPE_MISMATCH`; REQ-AMB-002 no time window is `INPUT_INVALID`), plan v1.9 (task OPS-07), DEC-2026-10-23, OD-P-70 (waiting for the Founder's "yes").
- noema#56 merged at `48f93f7`: five vendored copies re-pointed to docs `67a60ec`; `tools/vendor/test_repoint_bodies.py`.
- main at `48f93f7`: the first push runs failed in about 4 seconds with no steps and no runner (cause not seen; it cleared). All 12 workflows are green after one re-run each.

## Open

- OD-P-70 needs the Founder's "yes" before anything is built from it.
- Session D reports nothing wires `BENCH_OUT` for the per-call cost records (the compose owner).
- Review by the non-author founder is still needed on noema#42, #46, #47 and #59.
- Session C's noema#55 is superseded by #56; closing it is a founder action.

## Messages

Entries before this file existed are rebuilt from Session A's record of sent messages. "Receiver used it for" is "not recorded" unless the receiver said.

### 2026-10-21 17:04Z sent · Session A → Session C
- Purpose: answers to "Needs session A" items 1 and 5 on noema#49 (reconstruction response format).
- Receiver used it for: not recorded.
- Evidence: noema#50.

### 2026-10-21 17:07Z sent · Session A → Session C
- Purpose: reply to three gaps on noema#50 (ACTION items require `facts.grant_state`).
- Receiver used it for: not recorded.
- Evidence: noema#50.

### 2026-10-21 18:35Z sent · Session A → Session C
- Purpose: on the Founder's written instruction: the per-call cost source (Session D's model harness).
- Receiver used it for: not recorded.

### 2026-10-21 20:11Z sent · Session A → Session B
- Purpose: on the Founder's instruction: asks Session B to quote REQ-ENV-005 for "IMPOSSIBLE_STATE belongs to the proxy" in noema#51.
- Receiver used it for: not recorded.
- Evidence: noema#51; DEC-2026-10-22.

### 2026-10-22 14:23Z sent · Session A → Session D
- Purpose: status of Session D's "Needs session A" list (pack `understanding` block, `matcher_ref`).
- Receiver used it for: not recorded.
- Evidence: noema#59.

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
