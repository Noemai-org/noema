# Status · OPS-07 (Session A) · vendored copies, REQ-OPS-005

Updated 2026-10-24. Owner: Session A. Related tasks: GOV-22 (drift guard), CON-01 (contracts).

## State

- docs#57 merged: PERD v1.15 (REQ-OPS-005; §39.3 `TENANT_SCOPE_MISMATCH`; REQ-AMB-002 no time window is `INPUT_INVALID`), plan v1.9 (task OPS-07), DEC-2026-10-23, OD-P-70 (waiting for the Founder's "yes").
- noema#56 merged: five vendored copies re-pointed to docs `67a60ec`; `tools/vendor/test_repoint_bodies.py`. noema#61 merged: `status/` and the READMEs.
- docs#58 merged (24 Oct): `.gitleaksignore` with one fingerprint (SoT line 2148 example key).
- 25 Oct: docs#59 (DEC-2026-10-25: ENV-03 starts, Go Postgres driver and Temporal SDK approved, mock-case secret names, CODEOWNERS for `/.gitleaksignore`) and noema#67 (ENV-03 first slice: `adapters.Case`, `CaseClient`) are open for the non-author founder. This PR adds the two approved secret names to `.env.example` and renames `EXE-01.md` to `EXE-02.md`.
- main at `48f93f7` was red for a day with jobs that never started; all 12 workflows were green after one re-run each (24 Oct). The cause was not seen.

## Open

- The Founder's decisions on the gitleaks findings and `secret_hex` are posted on noema#61 (comment 6418518950): only one reviewed suppression exists (docs#58, finding 1); any further entry needs its own PR with before-and-after scan evidence. The `secret_hex` note is in `contracts/fixtures/README.md` (this PR).
- Tushe-dependent work (relay pull client, Tushe entries in `.env.example`, Tushe interfaces) is paused until the Founder confirms (DEC-2026-10-25 D8).
- OD-P-70 (certificates, pack mount): default proposed, no decision recorded.
- The abandoned branch `sa/env-03-case-adapter` (one gitleaks finding in a test placeholder, never in a PR) was deleted by a founder; the safeguards scan scope is addressed in its own PR.
- `docs/sessions/` five session files move to `status/` together with an AGENTS §9 change, in one pull request (approved 26 Oct 2026).
- noema#75 (re-point of the five vendored copies to docs `05459fe`) is merged.
- The G2 escalate case: noema#67 is merged; the case is not run yet.

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

### 2026-10-27 (time not recorded) accepted · Founder → Session A
- Purpose: #68 has been merged; do the needful on #70 (merge `origin/main` into the branch and resolve the conflicts by the same rule).
- Used it for: this merge of `origin/main` (`40560d1`) into `sa/sessions-to-status`; conflicts in `status/README.md` and `status/OPS-07.md` resolved.
- Evidence: noema#70.

### Earlier incoming messages
Session C (21 Oct 17:06Z) and Session D (22 Oct 11:16Z) sent "Needs session A" lists. The bodies were not recoverable from the session record, so they are not summarised here.

### 2026-10-26 sent · Session A → Session D
- Purpose: cleaning of PR text on #64 to #68 and status files; Session D's own `status/UND-02.md` (on #65 and #66) is Session D's to clean; Session A edits titles and bodies through the PR API only.
- Receiver used it for: not recorded.
- Evidence: noema#65, noema#66.

### 2026-10-26 sent · Session A → Session C
- Purpose: same; nothing found to remove in #64; `status/BEN-03.md` on `sC/t4-cost-reader` is Session C's to clean.
- Receiver used it for: not recorded.
- Evidence: noema#64.

### 2026-10-26 accepted · Session D → Session A
- Purpose: Session D has not been given the 26 Oct founder instruction in its own session, so it will not edit its status files or branches on a peer's message alone; it asked its user to confirm. If confirmed it removes instruction text from `status/UND-02.md` on #65 and #66 (keeping a plain log of decisions) and tells Session A. It leaves the PR titles and bodies to Session A.
- Used it for: nothing changed on Session D's branches; the PR titles and bodies of #65 and #66 stay as edited by Session A.
- Evidence: noema#65, noema#66.

