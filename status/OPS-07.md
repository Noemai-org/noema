# Status · OPS-07 (Session A) · vendored copies, REQ-OPS-005

Updated 2026-10-24. Owner: Session A. Related tasks: GOV-22 (drift guard), CON-01 (contracts).

## State

- docs#57 merged: PERD v1.15 (REQ-OPS-005; §39.3 `TENANT_SCOPE_MISMATCH`; REQ-AMB-002 no time window is `INPUT_INVALID`), plan v1.9 (task OPS-07), DEC-2026-10-23, OD-P-70 (waiting for the Founder's "yes").
- noema#56 merged at `48f93f7`: five vendored copies re-pointed to docs `67a60ec`; `tools/vendor/test_repoint_bodies.py`.
- main at `48f93f7`: the first push runs failed in about 4 seconds with no steps and no runner (cause not seen; it cleared). All 12 workflows are green after one re-run each.

## Open

- OD-P-70 needs the Founder's "yes" before anything is built from it.
- Session D reports nothing wires `BENCH_OUT` for the per-call cost records (the compose owner).
- Non-author founder review on noema#59 (the others, #42, #46 and #47, have merged).
- Session C's noema#55 is superseded by #56; closing it is a founder action.
- noema#61 (this log) needs the non-author founder's review.
- noema#75 (re-point to docs `05459fe`) is accepted; the Founder merges it.
- The G2 escalate case waits for the Founder to merge noema#67.

## Messages

The messages for other tasks are in their own files: `status/CON-01.md` (17:04Z and 17:07Z, 21 Oct), `status/EXE-01.md` (18:35Z, 21 Oct) and `status/PCK-01.md` (14:23Z, 22 Oct).

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

### 2026-10-26 (time not recorded) accepted · Founder → Session A
- Purpose: the nine-step instruction list of 26 Oct: post the decision comment on noema#61; clean the text of #64 to #68 and their status files; one fresh check on each final head of #67 and #68; limit the secret scan to the PR's own commits (#69); move `docs/sessions/` to `status/` and update AGENTS §9 (#70, docs#61); draft PERD v1.16 for §34.1 (docs#60); no clients for fault-controller or fixture-loader in core; run the G2 escalate case after #67 merges; no Tushe-dependent work until the Founder confirms.
- Used it for: docs#59, docs#60, docs#61, noema#67 to #70. The G2 case waits for #67 to merge.
- Evidence: docs#60, docs#61, noema#67, #68, #69, #70.

### 2026-10-27 (time not recorded) accepted · Founder → Session A
- Purpose: docs main is `05459fe` (includes docs#60 and docs#61); noema main is `08a2b25` after #69. Open one noema PR from main that re-points the five managed vendored copies with `tools/vendor/repoint_docs.py`, with the diff limited to those five files; report the head SHA and every check including docs-drift; do not merge.
- Used it for: noema#75, head `bab2586958f5a322c4bebcaa1b77b18e1f50f685`.
- Evidence: noema#75.

### 2026-10-27 (time not recorded) sent · Session A → Founder
- Purpose: report on noema#75: five files only; `repoint_docs.py --check --ignore-commit` exit 1 before and exit 0 after; 16 checks success, 1 skipped (`codegen-diff`), 0 failed, with run links; `docs-drift` does not run on pull requests, so it was dispatched once on the branch (run 40465546208, success); the message log was kept out of #75.
- Receiver used it for: the Founder accepted #75 and will merge it (next entry).
- Evidence: noema#75, comment 6436006042.

### 2026-10-27 (time not recorded) accepted · Founder → Session A
- Purpose: #75 is accepted and the Founder will merge it; keep it at five files; log the 26 Oct instruction, the #75 report and this message in `status/OPS-07.md` on #70, not #75; post the head SHA and check results on #70; stay idle until #67 merges, then run the G2 escalate case.
- Used it for: this entry, added to #70.
- Evidence: noema#70.

### 2026-10-27 (time not recorded) accepted · Founder → Session A
- Purpose: resolve the `status/README.md` conflicts on #67, #68 and #70, in that order: merge `origin/main` into each branch (no rebase, no force-push); keep main's task list plus the lines the PR adds; keep every row of the message index from both sides; keep the rules section; push; run checks once on the final head; report head SHA, what was kept from each side, and check results with run links on each PR; do not merge.
- Used it for: #67 was already merged (`f441ebf`), so nothing to resolve there. #68 merged main at `c3dea52`. #70 merged main in the commit that carries this entry.
- Evidence: noema#68, noema#70.

### 2026-10-27 (time not recorded) sent · Session A → Founder
- Purpose: report that #67 is already merged, and the per-PR results for #68 and #70 (head SHA, what was kept from each side, checks with run links).
- Receiver used it for: not recorded.
- Evidence: noema#68 and noema#70 comments.

### Earlier incoming messages
Session C (21 Oct 17:06Z) and Session D (22 Oct 11:16Z) sent "Needs session A" lists. The bodies were not recoverable from the session record, so they are not summarised here.
