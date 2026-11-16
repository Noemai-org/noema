# Status · ENV-04 (Integration Lead) · normalised adapter payloads

Updated 2026-11-06. Plan task ENV-04 (owner IL+AG; REQ-ENV-001, REQ-NFR-006). Ruling DEC-2026-11-06 A10 (proposed, docs#117): the Integration Lead owns the stand-in and the Tushe adapters, built on the PERD 34.4 shapes; the Tushe route mapping follows Tushe's final written response (R13, due Wed 11 Nov 2026).

## State

- Slice 1: the read ports `Transactions`, `Ledger`, `Switch`, `Destination` and the `fin.ledger_entry`, `fin.switch_record`, `fin.destination_status` payloads with `Validate` (`services/core/adapters/fin.go`, `fin_test.go`). SYNTHETIC label on the raw switch code is enforced.
- Not built: any client of hosted Tushe (no route is mapped; nothing is inferred from Tushe deck v4, which Noema has not observed); beneficiary and principal-context reads (PERD 34.4 fixes no normalised shape for them); (nothing).

- Slice 2 (same PR, 6 Nov): synthetic_tushe.StandinClient, the stand-in adapter A10 gives this role (Core Engineer 2 confirmed on noema#174 that they are not building it), to the Evidence Layer's expectations; Provenance (wire hash and label) on every read.

## Open points recorded, not decided

1. **Settlement states. Resolved by A31 (DEC-2026-11-06):** REVERSAL_PENDING is non-terminal and never SUCCEEDED, FAILED or REVERSED; a decision on it is wait or escalate. The port keeps it as its own state and `SettlementState.IsTerminal` says so (UNAVAILABLE stays a read result, not a state).
2. **`ReadError.Error()`** still says "case read failed" for every read; harmless, to be generalised in a later slice.

## Messages

### 2026-11-06 accepted · Founder → Integration Lead
- Purpose: work queue of 5 Nov 15:30: item 5, Tushe adapters per the committed decision records; collect Founder clicks into one numbered click-script.
- Used it for: this slice; R13 (ENV-05 pause lifted) and A10 as the authority; the route mapping left unbuilt because A10 says it follows Tushe's response.
- Evidence: this pull request.

# Session 2026-11-11 · ENV-04 (bench wiring, Compose and credentials side)

## Session summary
- What changed: `bench-runner` service (bench_net + edge_net, bench profile only) and its image; `bench-stack.yml` overlay (the core's stand-in settings, the proxy on the stand-in route table, the runner's output directory); `make bench-up` and `bench-down`; `profiles.py` resolves the bench profile with both overlays and has new rules and 11 new self-test mutations (the runner placement and networks, no bench credential volume on the core or the model); runner flags `--standin-credential-file` and `--proxy-credential-file`; the `bench-g1-stack` dispatch workflow (mock-case live log captured; not dispatched).
- Why: the Architect's order of 11 Nov (noema#441): the runner could not reach `bench_net`, three services generate three bench credentials, and the mock-case log must be a live file.
- Risks, and how they are contained: the runner joins `edge_net` and `bench_net` (the one bridge, by design); `profiles.py` fails if the core or the model ever join `bench_net` or mount a bench credential. The core settings are inert until the call site exists; the workflow ends NOT RUN until tokens and the call site exist, and fails on purpose.
- Tests added: `benchmark/runner/test_cli.py` PerServiceCredentials (4, red first); `profiles.py --self-test` (41 mutations, all caught).
- Conflicts raised: none. Not done, and not mine: session tokens (Edge), the core's read call site (Evidence Layer, EL/C2).

## Pre-work for the next agent
- Exact next step: when the Edge issues bench session tokens, add `--tokens-dir` to the workflow's runner line; when the core reads the stand-in, check the `NOEMA_STANDIN_*` names against its config.
- Open: the proxy's route table attributes the principal from a bare `X-Principal` header, which the adapter does not send (INV-12); the request log's principal field will be empty until the table follows the assertion (owner: IL, next).
- Files to read first: `deploy/compose/bench-stack.yml`, `deploy/compose/services/bench-runner.yml`, `.github/workflows/bench-g1-stack.yml`.
- Do not touch: the hostility proxy's data plane; `benchmark-heldout`.

## Session 2026-11-11 12:30 UTC · ENV-04 wiring slice W1, the Evidence Layer call site (C2)

## Session summary
- What changed: new package `services/core/evidence/portread` (code, tests, README): `Reader.ReadTransaction` reads the ledger, switch and destination ports of `adapters` and records one item each through `layer.Builder`; scope from the caller, descriptors from the caller, status from `layer.AssignStatus`. The task card is a comment on noema#441.
- Why: the Architect's instruction of 11 Nov (not yet in docs/decisions) that C2 owns the Evidence Layer call site for the stand-in; EL's question on noema#441 (no task row covered it).
- Risks, and how they are contained: four defaults (what makes a read determinate, no `fin.src.settlement` item, the scope strings, the payload shape) are proposals awaiting a ruling and are listed in the README and on noema#441. Nothing calls the reader yet: `cmd/core` wiring and the Compose bench profile are IL's and wait for EDG-01 slice 3b.
- Tests added: `reader_test.go` (REQ-CTX-002, INV-12, INV-19, AGENT-25, EC-E16): healthy answers KNOWN with the right descriptor and scope; six non-final answers UNKNOWN; every adapter failure class an item with the right status and hash or no-bytes; unusable answers; request and configuration refusals read nothing; per-call deadline; a refused Record stops the reads. The tests were written after the code (no red-then-green pair); six mutations of the code were each caught (pending credit counted as final, empty ledger determinate, refused Record skipped, failed-read hash dropped, non-terminal switch success determinate, a record for another transaction accepted).
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: slice 2, an end-to-end test with the real `StandinClient`, the real stand-in and Postgres, asserting items and `EVIDENCE_READ` events; then IL wires `cmd/core` after EDG-01 slice 3b.
- Open questions (owner): the four defaults (Architect or EL); which module "input gateway wiring" means (Architect).
- Do not touch: `adapters/**`, migrations, contracts, packs.

# Session 2026-11-13 · ENV-04 W1 · Codex's P1 on 89ee1cf9: the four defaults had no committed ruling

- **Finding:** NOT READY (P1): the reader hard-codes what counts as a determinate ledger, switch and destination result, and the PR said the four defaults await a ruling.
- **Resolved by a committed record, not by code:** ruling A148 (Architect, 12 Nov 2026) accepts the four defaults; it is in `docs/decisions/DEC-2026-11-12-architect-rulings-a148-a160`, merged in docs#157 on 12 Nov 21:42 UTC. Task ownership is A146 (docs#156, merged). The one change in this PR is the README line, which now cites A148 instead of saying the defaults await a ruling. No code or test changed.
- **A reading stated, for the Architect to correct:** A148's first clause ("a read is final when the ledger, switch and destination reads all return KNOWN status") is implemented as each port's item being KNOWN only when its own answer is final; the package returns no combined flag, and the caller (Trust's input) derives it from the three items. If a returned flag is meant, I add it test-first.
- Debt: none. Conflicts raised: none.

# Session 2026-11-13 22:20 WAT · ENV-04 W2 · the Evidence adapter (C2)

## Session summary
- What changed (PR noema#548): new package `services/core/evidence/readseal` (code, tests, README): `Adapter.Read` implements `orchestrator.Evidence` over the intent store, `portread` and the snapshot sealer, with a `Reconciler` interface that has no default.
- Why: the Founder's order of 13 Nov 2026 ("start the Evidence adapter PR now"); EL named it mine on noema#507 and noema#436 section A.
- Risks, and how they are contained: the real classifier is not built (the pack's evidence-source descriptors are empty); until it is, the adapter cannot be wired into `cmd/core`, so it adds no runtime behaviour. The package, the Trust-login transaction and what `Unavailable` means follow my recommendations on noema#547; EL has not answered yet.
- Tests added (REQ-CTX-001, REQ-CTX-004, REQ-AUD-001; INV-11, INV-12, INV-14, INV-17, INV-19): 17 tests in `readseal_test.go`; the first 15 were committed alone as `d2c4ae7f` and failed in CI. Two were added with the implementation (the slot and kind come from configuration; an incomplete classification) and ten mutants were killed locally (one survived and gained a case).
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: EL's answer on noema#547; then slice 2, the real `Reconciler` (needs the evidence-source descriptors: who owns them?); then EL plugs the adapter into `cmd/core`. A database test of the real sealer under the adapter can follow once point 2 of noema#547 is answered.
- Open questions (owner): noema#547 points 1 to 4 (EL).
- Files to read first: `services/core/evidence/readseal/README.md`, `.../readseal.go`.
- Do not touch: `portread`, `snapshot`, `orchestrator`.

# Session 2026-11-14 11:15 WAT · ENV-04 W2 · the Security Reviewer's NOT READY on noema#548

- **Blocking (the principal boundary): fixed.** `Read` now requires the stored intent to name exactly the session's principal (`principal.principal_id`); an intent of another principal, of another tenant, or one that names no provable principal (no member, not text, empty, wrong case, not JSON) answers as a missing intent (`intentstore.ErrNotFound`), before any read, classify or seal. `ErrTenant` stays for a session of another tenant than the adapter's. Failing first: three tests at `5cd70576` (the `core` job failed, run 43586400411). Four mutants of the new code were run: three killed; "an empty principal accepted" survived and is equivalent (the session's principal is never empty: `ErrNoSession` is checked first).
- **Note 4:** migration 0020 checks the shape of `reconciliation_id` (`^rec_...`) and `reconciliation_classification` (`^[A-Z][A-Z0-9_]*$`) and not a closed set; the adapter now refuses an answer that is not of those shapes (`ErrIncomplete`); the closed set stays the Reconciler's contract. **Notes 3 and 5:** stated in the README (who bounds what; the caller maps every error to one answer and never returns `err.Error()`). The cross-tenant and cross-principal test over Postgres belongs to the first real wiring.
- Debt: none. Conflicts raised: none.

# Session 2026-11-16 · ENV-04 slice 2 · the evidence item carries its source and times (noema#596)

## Session summary
- What changed (PR noema#613): `evidence/layer/builder.go`: `layer.Item` gains `Source`, `ReadAt`, `EffectiveAt` and `FreshnessSLASeconds`, set by `Builder.Record` from what it just wrote to `evidence_item`; `builder_test.go`: one test that reads the stored row back and compares.
- Why: the Founder's order of 16 Nov 2026 ("Reconciler slice 2 per A228/#596"). The Reconciler builds a `reconcile.Evidence` per mapped row and needs the source, the read time (for `ExpiresAt`) and the effective time, which `layer.Item` did not carry.
- Risks, and how they are contained: additive to a struct whose literals use field names; no new read, no new login; no Record event changes.
- Tests added (REQ-CTX-001, REQ-RES-001): 1, red first (run 43806709349: `static`, `tests (2)` and `core` failed with "item.Source undefined"; the whole run read, `compose` included).
- Regression cases added: the item agrees with the stored row.
- The Security Reviewer's READY on noema#613 at `1ba21a85`, with a conflict and three notes (16 Nov 2026): merged `main` for the conflict (`status/ENV-04.md`, both sessions kept). Note 2: the item's `FreshnessSLASeconds` was only the source's limit, while `AssignStatus` applies the stricter of the source's and the profile's (INV-23), so an expiry made from it would have been looser than the status: the field is now `EffectiveFreshnessSeconds` (the applied limit; the row's `freshness_sla_seconds` stays the source's). Note 3: the item's times are cut to the microsecond `timestamptz` keeps. Both tests first (`c04783c8`). Note 1 (a zero `ReadAt` or `EffectiveAt` is refused nowhere): not changed here, because the builder never refused it and a failed read may carry no time; the Reconciler (slice 2b) refuses a zero value it uses, and the field's comment says so.
- Debt entries: none. Conflicts raised: none.
- Default taken (AGENT-02), stated on noema#596: EL owns `evidence/layer`; EL has not answered question 1, so this PR is the smallest default and closes if EL prefers to add the fields.

## Pre-work for the next agent
- Exact next step: after noema#589 merges, the `PayloadReader` port in `readseal`, the hash check (decode with exact numbers, canonicalise, compare with `PayloadHash`), and the Reconciler (`Classify`), tests first. A row with no fields is not a positive fact (INV-19; the Security Reviewer's note 2 on noema#589).
- Open questions (owner): `Required` per row (Architect, noema#579); where the reconciliation id comes from (EL, noema#596).
- Files to read first: `trust/reconcile/types.go`, `trust/reconcile/mapping/mapping.go`, `evidence/readseal/readseal.go`, `cmd/core/payloadreader.go`.
- Do not touch: the pack's mapping rows until the Architect accepts them.

# Session 2026-11-15 11:00 WAT · ENV-04 · the G1 stack workflow's session tokens and leak scan (IL, noema#577)

## Session summary
- What changed (PR noema#577): the stack workflow makes a throw-away synthetic key pair with the Edge's issuer (`make bench-tokens`), mounts the PUBLIC key in the core only, and issues each principal's token in the runner; the inputs are validated on the whole string; every uploaded file is leak-scanned with an allow file of 13 anchored patterns and nothing is uploaded unless the scan is clean; the core and the token script name the key id and audience the same way (`BENCH_KID`, `BENCH_AUDIENCE`); netcheck refuses any other service that mounts the bench output directory or a folder above it.
- Why: the Founders' order of 15 Nov (G1 session tokens, noema#436 row 2) and the Security Reviewer's, C2's and Codex's reads of noema#577.
- Risks, and how they are contained: the stack itself has not been run (no Docker here); the first real run is the G1 dispatch. The allow file exempts only named numeric fields and the scorer's own metadata lines, each anchored to the whole line and at most 9 digits; the scan cannot scope an allow pattern to a path, which is accepted risk (noted on noema#591).
- Tests added: 16 tests in `benchmark/runner/test_g1_workflow.py` (the validation step is run in bash against 13 good and bad values; the scorer's own CLI output is scanned; each allow pattern and each guard has a mutation that fails a test).
- Regression cases added: none beyond the tests. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: merge noema#577 before noema#591 (both edit the workflow, its test and the allow file); then merge `main` into noema#591 and keep its fuller scan.
- Open questions (owner): the concurrency group is single-flight (`bench-g1-stack`); the Security Reviewer asks for one ruling (Architect or Founders).
- Files to read first: `.github/workflows/bench-g1-stack.yml`, `tools/ci/g1_leakscan.allow`, `benchmark/runner/test_g1_workflow.py`.
- Do not touch: the private key path and the issuer binary (runner only).
