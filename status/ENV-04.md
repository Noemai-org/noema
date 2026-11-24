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

# Session 2026-11-14 17:20 WAT · ENV-04 · the evidence mapping (A208, slice 1 of 2, noema#589)

## Session summary
- What changed (PR noema#589): `services/core/trust/reconcile/mapping` (`Parse`, `Rows`) and `packs/fintech/evidence_mapping/mapping.v1.json`: versioned pack data that turns a stored source payload (`entries`, `semantic_status`, `credit_state`, the words `portread` records) into rows of the reconciliation rule set's vocabulary (`debit`, `switch`, `credit`).
- Why: the Founder's order of 14 Nov 2026 ("the evidence mapping is versioned pack data: C2 owns it and EL reviews it; classify first, then seal (A208)"). EL's finding on noema#547 and my proposal on noema#579: nothing mapped the payloads to the rule set, so the adapter's `Reconciler` could not classify.
- Risks, and how they are contained: **absence is never a negative fact** (INV-19): a field the payload does not give, or gives as a value the mapping does not list, is left unset; nothing is defaulted; `debit.NOT_POSTED` is never produced. A mapping that is not whole fails at load (unknown entity or field, non-enum, a value not in the enum, an unlisted source, a repeated row or member, another rule set). The status of a row is the evidence item's, never the mapping's.
- Tests added (REQ-RES-001, REQ-CTX-001; INV-19, INV-21): 12 test functions, red first (run 43614711290, `static` failed: no non-test Go files). Twenty hand mutations: seventeen killed; three survive and are equivalent (a missing entity check, a missing field check and a missing non-enum check are each refused again by a later check). Two cases were added after the red push: a repeated JSON member at any depth, and a quantifier value outside the enum (the one real survivor).
- Regression cases added: a repeated member at any depth is refused; a quantifier whose value is not in the enum is refused.
- Debt entries: none. Conflicts raised: none. Open: the Architect's reading of A208 (noema#579: the predicate language or this closed reading) and approval of each row's meaning; EL's review of the pack data.

## Pre-work for the next agent
- Exact next step: slice 2, the `Reconciler` (readseal's port): load each item's stored payload (`evidence_payload.json`, SELECT is granted to `trust_rw`), call `Rows`, set each row's `Evidence.Status` from the item, call `reconcile.Reconcile` with the rule set, record `mapping.ID()` beside the rule set's id, return `Classified`. The payload reader needs a Trust-login connection: ask EL where it lives.
- Files to read first: `trust/reconcile/mapping/mapping.go`, `packs/fintech/evidence_mapping/mapping.v1.json`, `evidence/readseal/readseal.go` (the `Reconciler` interface), `evidence/portread/reader.go` (the payload shapes).
- Do not touch: the rule set (`rules.v1.json`) and the semantic table until the Architect approves it.

# Session 2026-11-15 · ENV-04 · the Security Reviewer's blocking finding on noema#589 (the mapping's version)

## Session summary
- What changed (PR noema#589, two pushes: the tests, red first, then the fix): `mapping.Parse` takes the identity and version the caller expects (`<mapping_id>@<version>`, from the pack manifest at composition) and refuses any other, so a file that is silently another version, or whose id and version disagree with what was approved, never loads. The label must be exactly `SYNTHETIC` (note 1). The document is bounded before it is decoded (note 3): `Parse` takes `Limits{MaxBytes, MaxDepth}` from the caller (platform/config; the repository's own lint refuses a threshold literal in decision code, which I learned from the first push of this round) and refuses limits that are not positive. The two loops the Security Reviewer called vacuous now fail if no row or no field is produced (note 2).
- Why: the mapping is the pack data that decides what a source payload means to Trust (AGENT-16); a version change must fail a test, not pass silently.
- Risks, and how they are contained: the composition must pass the expected identity from the pack manifest; **there is no caller yet**, so nothing breaks, and the Reconciler (slice 2) takes it as an argument. Hand mutants on the new code, all killed (the label check, the identity check, the version in the identity, both bounds, the limits check, the depth).
- Tests added (REQ-RES-001, REQ-CTX-001; INV-19, INV-21, AGENT-16): three new test functions and two existing loops made non-vacuous. Red first: the package did not compile against them (a signature change), so the red shows the API, not each assertion; the mutants are the evidence for the assertions.
- Not done, stated: a JSON Schema for the mapping file with valid and invalid fixtures, or a hash pin (the Security Reviewer's "can follow"); the Architect's acceptance of each row's meaning (A208, noema#579) remains open.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the Reconciler (slice 2) calls `Parse` with the expected `<id>@<version>` the pack manifest names; see noema#596 for the inputs it still needs.

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

# Session 2026-11-16 · ENV-04 slice 2a · the verified payload read (noema#596)

## Session summary
- What changed (PR noema#619): `evidence/payloadread/` (`Reader.Payload`, typed errors, README), `payloadread_test.go`.
- Why: the Founder's order of 16 Nov 2026 ("Reconciler slice 2, #599 is merged"). The reader of noema#599 returns `jsonb` text; the item's hash covers the canonical form; the Reconciler needs a payload it can prove is the sealed one. This slice depends on nothing unmerged.
- Risks, and how they are contained: fails closed on every error and returns no object; no payload in an error; the tenant is the caller's; a read of any kind that cannot be verified is "cannot be classified".
- Tests added (REQ-CTX-001, REQ-AUD-001; INV-12, INV-19, INV-21): 12 tests incl. 300 generated payloads, red first (run on `e206f36f`: the package did not build); 15 hand mutants, all killed.
- Regression cases added: a changed value; another payload's hash; trailing data; a fraction; an integer at 2^53-1; every read failure; each unusable call reads nothing.
- Debt entries: none. Conflicts raised: none.
- Defaults taken (AGENT-02): any failure of the read is one error (`ErrUnavailable`) wrapping the cause, because EL's error values live in `package main` and cannot be imported; the payload is not size-bounded here (the reader's bound is noema#606).

## Pre-work for the next agent
- Exact next step: slice 2b, the `Classify` (`evidence/reconciler`): after noema#589 (mapping) and noema#613 (`layer.Item` source and times) merge, build one `reconcile.Evidence` per mapped row (a row with no fields is not a positive fact, INV-19), `Required` per the Architect's answer on noema#579 (default until then: every item required, the stricter reading), `now` from `platform/clock`, `rec_` plus letters from SHA-256 of the rule set, the mapping and the sorted evidence IDs, then `reconcile.Reconcile`. It needs `mapping` to say which entities a source feeds (for an item with no payload).
- Open questions (owner): `Required` per row (Architect, noema#579); where the reconciliation id comes from (EL, noema#596).
- Files to read first: `evidence/payloadread/README.md`, `evidence/readseal/readseal.go` (the `Reconciler` interface), `trust/reconcile/types.go`, `trust/reconcile/mapping/mapping.go`.
- Do not touch: the mapping's rows until the Architect accepts them.

# Session 2026-11-17 · ENV-04 slice 2b · the Reconciler (noema#663; A208, A245, A260)

## Session summary
- What changed (noema#663): `services/core/evidence/reconciler/` (`Reconciler.Classify`, README, tests). It reads a KNOWN item's verified payload, maps it with the pack's mapping, builds one `reconcile.Evidence` per row (every item required, A245), classifies with the real rule set and returns `rec_` plus 26 letters and the classification.
- Why: `readseal.Reconciler` had no implementation, so the Evidence adapter could not be wired (noema#547, noema#596). A260: absence is UNKNOWN.
- Risks, and how they are contained: it sits under Trust's evidence classification. It never turns an absence into a fact (a row with no field, an unverifiable payload, a non-KNOWN item cannot satisfy a rule); a store failure refuses the whole call; its error texts are fixed. It is not wired (`cmd/core` is EL's).
- Tests added (REQ-RES-001, REQ-CTX-001, INV-19, INV-21, INV-23): 15 test functions with subtests; 23 hand mutants, all killed but two that are equivalent (an empty source is also refused by the mapping; the clock's reading feeds only `ValidUntil`, which `readseal` does not take). Six survived the first tests and each got a case (an empty tenant, a required unavailable source, the item status and the mapping in the id, a trusted row with no field, the expiry).
- Regression cases added: the same.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: EL's composition root builds `Reconciler` from `ParseRuleSet`, `mapping.Parse` (with the expected `<id>@<version>` the pack manifest names and the pack's evidence source ids), `payloadread.Reader{Payloads: trustPayloads}` and the clock, and plugs it into `readseal.Adapter`; then the G1 story test.
- Open questions (owner): none (A245 and A260 answered `Required` and absence).
- Files to read first: `evidence/reconciler/reconciler.go`, `evidence/readseal/readseal.go`.
- Do not touch: the mapping's rows (accepted by A245; a change goes through the Architect).

# Session 2026-11-17 · ENV-04 · fixes for the reads of noema#663 (Security Reviewer and EL)

- **P2 and note 2 (done, tests first):** the reconciliation id now hashes the clock reading and, per item, the source, read time, effective time and freshness limit, as well as the id, payload hash and status. `reconciler_review_test.go` (the id follows every input, 7 cases; and the id is stable for the same inputs) was red at the first push. A retry at a later clock reading therefore has another id: `readseal` stores the classification on its own snapshot, so nothing collides.
- **Note 3 of the Security Reviewer (done, tests first):** an item whose `ReadAt` is after the clock's reading is refused whole (`ErrItem`); a read at the reading is used. I chose "refuse" of the two options offered because it fails closed without needing the layer's clock to be the same one; whether `layer` takes `ReadAt` from the injected clock is for the layer's owner to confirm (`builder.go` takes it from the read object it is given).
- **Tamper signal (open, not decided):** a payload that does not match its sealed hash becomes UNKNOWN silently (A260). Where it is surfaced (a Record event or a metric) is for REC and SEC to decide; this package does not emit one. It is DEBT-44 (the Founder's order of 17 Nov 2026); A287 settles the earlier findings.
- **EL note 2 (done):** README says the freshness bound is the layer's. **EL note 3 (answered, not coded):** in the real mapping each source feeds exactly one row, so "one row's field missing while another row of the same source is present" cannot occur end to end; the per-row path is in `internal_test.go`. A test with a second row for a source needs a mapping row the Architect has not approved (A245).
- Debt: DEBT-44 (the tamper signal). Conflicts raised: none.

# Session 2026-11-18 · ENV-04

## Session summary
- What changed: `bench-g1-stack.yml` gains `mode` (full or stack-check), SR's first-dispatch condition 1 (`tools/ci/g1_record_access.py`: exactly one SERVED RECORD_ACCESSED line per case, IDs and outcome only, empty log is NOT RUN) and condition 2 (a `joblog-scan` job that reads the finished job's log through the API and runs the leak scanner with its canary).
- Why: A323 and A325 as relayed by the Founders; the rulings are not yet committed in `docs`, so the exact conditions are my reading of SR's words (see Open questions).
- Risks, and how they are contained: stack-check runs no suite and is labelled NOT EVIDENCE in the summary and the Result step; the new job has `actions: read` only.
- Tests added: `tools/ci/test_g1_record_access.py` (8), four workflow guards and mode validation cases in `benchmark/runner/test_g1_workflow.py`. Seen failing first.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: first live run is the proof; the `joblog-scan` log endpoint and the stack-check health rows are unverified (no Docker or Actions here).
- Open questions (SR): is "exactly one SERVED per case" the intended count, and is the g1-stack job log the full scope of condition 2?
- Do not touch: the held-out set.

# Session 2026-11-19 · ENV-04 · the stack-check rename and Codex's two P2s on noema#730 (IL)
- Renamed the dispatch mode `smoke` to `stack-check` (A354 to A356 as relayed; the text is not yet in `docs`): `full` is the only mode that can be evidence; `stack-check` brings the stack up and checks that every service runs and is healthy, runs no suite, scores nothing and says so.
- Codex P2 (a healthy stack-check run failed in the scan): the scan list is fixed per mode; `record-accessed.txt` is added only for `full`.
- Codex P2 (the job log's own digits, among them the seed in the command line, the run id, the image version and the dispatch inputs the runner echoes, would fail the job-log scan): `tools/ci/leakscan.py --no-digit-rule` turns off the digit-run rule and nothing else; the job-log scan uses it, with no allow rule; `g1_leakscan.py --no-digit-rule` probes only the kinds that remain (canary, api key, JWT-shaped, bearer token) and is NOT RUN if one is not reported. Tests: the scanner, the probe (each remaining kind dropped by a wrapper is refused) and two workflow guards (the flag is used once, only for the job log; the scan list per mode).
- Not verified: the whole job-log path (the API's log endpoint and the real log's content) until the first live run.

# Session 2026-11-19 · ENV-04 · noema#730: the request-log count, the pinned canary and the saved scan output (IL)
- RECORD_ACCESSED (A354): `tools/ci/g1_record_access.py` takes `--requests benchmark/out/run/requests.jsonl` (the runner's own per-request record; format proposed by BO: case_id, correlation_id, method, path, status, kind in case, reconstruction or probe; no header, body or token). The expected SERVED count per correlation is the number of `reconstruction` requests answered 200; a probe or a refused read adds nothing. A missing, empty or no-reconstruction record is NOT RUN; an extra field, a bad status or kind, or a correlation outside the run is NOT MET. The writer is BO's (runner). Mutations: expected count fixed at 1, and counting non-200 reads: both caught.
- Canary (A354): one fixed synthetic value from split literals (`g1_leakscan.CANARY`), SHA-256 asserted by a test; the scan reports the kind and never the value (existing tests kept).
- The scan's own stdout is saved to `benchmark/out/leak-scan.txt` (the exit code survives the pipe by PIPESTATUS) and uploaded after a clean scan, for BO's package row 8.
- Not available from the mock-case log: write counts. mock-case logs only refusals; the writes are in its command log behind the truth listener, which only the runner may read (BO's oracle client).

# Session 2026-11-19 · ENV-04 · browser checks after the stack is up (A388, IL)
- `bench-g1-stack.yml` gains an opt-in boolean `playwright` (default off). When set, a step runs `npm ci` and `npx playwright test -c playwright.stack.config.mjs` in `ui/` against the stack's UI on 127.0.0.1:8082, before the stack stops; without `ui/playwright.stack.config.mjs` (FE owns it and the specs) it is NOT RUN (3), never a pass. A requested check that is not 0 fails the Result step; the checks are labelled not evidence for G1. Opt-in until the wiring merges.
- Not verified: no live run; FE's stack config and specs do not exist yet.

# Session 2026-11-20 · ENV-04 · the stamp for the G1 package (noema#717; IL)
- The first line of `benchmark/out/leak-scan.txt` is `stamp: <dispatch sha> <run id>`, and `fault-plans.json` and `negative-probes.json` carry a top-level `"stamp": {"sha", "run_id"}` (`benchmark.runner.evidence --sha --run-id`: 40 lowercase hexadecimal and digits, both or neither, refused before anything is read). `reconstructions.json` is BO's writer: BO stamps it the same way.

# Session 2026-11-20 · ENV-04 · Codex and SR on noema#730 (IL)
- The rulings behind the conditions are committed now: A323 and A325 in `docs/decisions/DEC-2026-11-18-architect-rulings-a317-a328.md`, A354 and A355 in `DEC-2026-11-19-architect-rulings-a341-a357.md`. The earlier line "the rulings are not yet committed" is superseded by those records. Gap against A323's text: A323 says the stack-check also checks `/readyz` and the netcheck; as built it checks that every service is running and, where it has a health check, healthy. `/readyz` and the netcheck are not in the workflow yet (follow-up).
- Codex P2 (the evidence's unknown-correlation probe made the request record NOT MET): a request of kind `probe` may name a correlation outside the run; a `reconstruction` or `case` request may not.
- Codex P2 (the stamp's run id is a digit run the artifact scan found): one anchored allow pattern for the `"run_id"` line (14 patterns). The SHA is one hexadecimal token, which the digit rule does not read as a run, so it needs none.
- Not changed: `print` in the CI checkers (AGENTS §7's rule is for service code; every `tools/ci` CLI writes its result to stdout, 54 call sites), and the request-record writer (BO's, agreed by message; until it exists a full run is NOT RUN, never a pass).

# Session 2026-11-20 · ENV-04 · A445 on noema#730: readiness reported by the stack-check (IL)
- The stack-check now reports `/readyz` (asked from the runner, on the core's network) and the static netcheck (`deploy/compose/netcheck/profiles.py`) in the job log, the Summary and step outputs. `READINESS_REQUIRED` in the Result step is `"false"` now; the final composition PR flips it, after which a core that does not answer 200 or a failing netcheck fails the stack-check (A445, interim). The `print` rule is for `services/` (A445): Codex's P1 on this PR is answered on that ruling.
- Not verified: no live run; the runner's `http.client` call and the netcheck on the Actions runner.

# Session 2026-11-20 · ENV-04 · BO's package step (A444, noema#821; IL)
- A `package` step after the evidence and before the leak scan (full mode): `python -m benchmark.runner.package_artifacts --run /bench-out/run --sha $GITHUB_SHA --run-id $GITHUB_RUN_ID --probes /bench-out/evidence/negative-probes.json` in the runner container. It writes `stamp.json` and `mock-case-writes.json` into the run directory (so they are scanned and uploaded) and stamps the probes file. The Result step fails the run unless it exits 0 (1: a case whose writes could not be counted; 2: unusable input). Depends on noema#821 merging first: until the module exists a full run fails at this step.
- `reconstructions.json` and `requests.jsonl` stay BO's (runner writers).

# Session 2026-11-19 · ENV-04 · the A356 bench settings and the per-tenant Trust login files (IL)
- `bench-stack.yml`: the A356 table as bench core values (`NOEMA_TRUST_AT_RISK_BPS` removed as corrected: trustlimits reads it from the pack), the three paths, and a one-shot `trust-dsn-init` (no network, root, secret `noema_tenant_login_password`) that writes one `trust_rw__<tenant>` DSN file per tenant into the `trust_dsns` volume at mode 0400, owner 65532; the core mounts it read-only and waits for the step.
- netcheck: `trust-dsn-init` is a sidecar exempt by exact name (`ONE_SHOT_NAMES`); only postgres and that step hold the password; only that step (writable) and the core (read-only) mount the volume; the core's A356 values are checked by number; 10 new self-test mutations (124, all caught).
- Not done here (waits): `policy-publish` and `tenant-init` one-shots (C1's script and #771 are not on main; the core-trust key provisioning script is not found), `NOEMA_PACK_MANIFEST` (needs the core's loader), `NOEMA_GRANT_KEY_DIR` is set but unread until EL wires it.
- Not run locally: `trust-dsn/test_init.py` (needs Linux; no Docker daemon here): CI is its proof.

# Session 2026-11-19 · ENV-04 · the optional Intron key mount (A377 to A379, IL)
- `deploy/compose/bench-intron.yml` (optional overlay, added by `make bench-up` only when `NOEMA_BENCH_INTRON_KEY_FILE` is set): `INTRON_API_KEY_FILE=/run/intron/key` and one read-only bind of the host file into the model service. No dummy file, no environment variable for the key. Facts from Voice (noema#689's reader): unset means the provider is not registered and the service starts; a set variable with a bad file fails at call time; the file needs mode 0600 or 0400 and owner uid 10001.
- netcheck resolves the bench stack with the overlay too and checks it (variable and mount together, bench only, one read-only bind of one file at that path, never a folder or volume; the core cannot mount it); 8 new mutations (132 total, all caught).
- Not done: the workflows' own compose commands do not add the overlay (no Environment file path yet); the bench Environment writing the file, owned by the user the bench model runs as (the invoking user, NOEMA_BENCH_UID, not 10001; Codex on noema#790), is the runner's step when the key exists.

# Session 2026-11-20 · ENV-04 · SR's two hardening items on noema#790 (IL)
- `trust-dsn-init` now has `cap_drop: [ALL]` and `cap_add: [CHOWN, FOWNER, SETUID, SETGID]` (give the file to the core's user, set its mode after it is no longer the step's own, read it back as that user), and the read-back child drops supplementary groups (`os.setgroups([])`) before `setgid`/`setuid`. netcheck requires exactly that set (3 mutations; 135 in all, caught) and the overlay text rule now fails on no input (checked by the self-test).
- Not verified at run time: no Docker daemon here. If a capability is missing the step fails closed and the core does not start; the first stack-check shows it. `grant-keys-init` (merged) has neither hardening; a follow-up is SR's call.

# Session 2026-11-20 · ENV-04 · Codex and SR on noema#790 (IL)
- Codex P1 (fresh checkout, `NOEMA_TENANTS` unset): postgres and `trust-dsn-init` fell back to the stand-in tenant but `secrets_local.sh` read only `NOEMA_TENANTS`, so the emitter-login secret was never made and `init.sh` refused. `make bench-up` now uses one effective list (`BENCH_TENANTS`, NOEMA_TENANTS else BENCH_TENANT) for the secret step and for Compose; `deploy/compose/test_bench_tenants.py` (run in `compose-scripts`) reads the real Makefile and overlay. Not done and not mine to guess: mapping the tenant logins in the database (`provision-tenants.sh` after the migrations) is C2's compose sequencing.
- Codex P2 (the key file owner): `bench-model.yml` runs the model as the invoking user, so the key must be owned by that user, not by 10001. The overlay comment, the README and this file say so. Voice's "10001" was the image's own user.

# Session 2026-11-20 · ENV-04 · A445 on noema#790 (IL)
- `NOEMA_PACK_MANIFEST=fintech/manifest.v1.json`, `NOEMA_PACK_MAX_BYTES=1048576`, `NOEMA_PACK_MAX_DEPTH=32` in the bench core settings (the Architect's bench values; the largest pack file is 64,069 bytes), checked by number in netcheck (137 mutations). `secrets_local.sh` makes the core-trust signing seed (`secrets/core_trust_ed25519_seed_1`, 64 lower-case hexadecimal, mode 600, kept on a second run, never printed): the form `signing.LoadRing` reads from `core-trust.ed25519.<version>.key`. A one-shot that sets its mode and owner, the `policy-publish` step and the netcheck rule "no other service lists or mounts the core-trust slot" come with #771 and C1's tenant-init: there is no consumer to mount it yet, and no Compose secret names it, so nothing holds it.
- Not run here: `test_secrets_local.sh` (the whole script exceeds my local time limit on Windows); the new lines were checked in isolation.

# Session 2026-11-19 · ENV-04 G1 evidence: SR's two first-dispatch conditions (noema#436, A323)
## Session summary
- What changed: `benchmark/runner/evidence.py` (each probe has its own principal, letters only; the unknown correlation is recorded; new `record-access` command), `benchmark/runner/test_evidence.py`, `.github/workflows/bench-g1-stack.yml` (job-log capture, `access` step, show step after a clean scan, Result fails on the access check), `benchmark/runner/test_g1_workflow.py`.
- Why: SR's conditions of 15 Nov: (1) the core's access log must show the reconstruction handler ran for the control and the unknown-correlation probe and for no refusal; (2) the job log is as public as an artifact, so runner, generator, scorer and evidence output goes to files that the leak scan covers and is shown only after a clean scan.
- Risks, and how they are contained: the workflow is IL's file (A323); this edit is the conditions only, no smoke mode. `make bench-up` output (image builds, paths) still goes to the job log: it carries no case data. A probe principal has no digit, so it adds no digit run to the core log or the scan.
- Tests added (REQ-SEC-004, INV-12, INV-22; criterion 9 of noema#436): `test_evidence.py` (ProbePrincipals, RecordAccess: 9 tests), `test_g1_workflow.py` (3 tests). Mutants: 12 of the evidence logic and 7 of the workflow, each killed.
## Pre-work for the next agent
- Exact next step: IL merges main into the smoke-mode change over these step names; the first dispatch counts only after Codex, SR and a Founder.
- Do not touch: the leak scan's planted kinds.

# Session 2026-11-20 · ENV-04 · noema#730 merged with main (IL)
- Merged `origin/main` once (A468: a merge-main commit voids nothing). Main now has noema#727's design: the runner, generator, scorer and evidence write to `benchmark/out/joblog/*.log` (nothing they print reaches the job log), and a `record-access` evidence step (criterion 9, second half; step id `access`). This PR keeps both: that step now runs in full mode only, and the request-record count of A354 is a separate step `count` (`tools/ci/g1_record_access.py --requests`). Every `$compose exec` of this PR (the `/readyz` probe, the generator, the evidence stamps and BO's `package_artifacts`) ends in a joblog redirect, as main's test requires.
- The stack-check's `/readyz` answer is read from `benchmark/out/joblog/readyz.log`; the allow file has 15 patterns (main's 14 plus the stamp's `run_id` line); `evidence.py` takes `--sha`/`--run-id` on fault-plans and probes and still takes none on `record-access`.
- Offline: `test_g1_workflow` and `test_evidence` (66), `test_g1_record_access` 14, `test_g1_leakscan` 16, lint OK. Not run: the workflow.
- Codex P2 (the package step called Compose without `NOEMA_BENCH_OUT`, `NOEMA_BENCH_UID`, `NOEMA_BENCH_GID` and `NOEMA_BENCH_MAX_BODY_BYTES`, which bench-stack.yml requires): the step now exports the same four, and a test requires them in every step that defines the compose command (red first).

# Session 2026-11-20 · ENV-04 · Codex's `.env` finding on noema#790 (IL)
- Codex P2 (twice): the stand-in fallback overrode a tenant list set only in Compose's `.env`. `deploy/compose/bench_tenants.sh` now gives the one effective list (the shell, then `.env`, then the stand-in tenant; an empty result is no list) and `make bench-up` passes it to `secrets_local.sh` and to Compose (`BENCH_TENANTS`). `test_bench_tenants.py` runs the script on ten cases (shell wins, `.env` only, quoted, comment, look-alike, empty shell value, the fallback as an argument) and reads the real Makefile. Merged main once (conflict only in `compose-scripts.yml`, both sides kept).

# Session 2026-11-20 · ENV-04 · the independent reads of noema#790 at `8e4865e7` (IL)
- SR P2 and C1 P2: the tenant list is pasted into the recipe as text, so a `.env` value with a quote or a `$(...)` would run as shell. `bench_tenants.sh` now checks every word (and the stand-in tenant) against the tenant shape and prints nothing and exits non-zero otherwise; `bench-up` stops on an empty list before anything runs. A test with fifteen hostile values, in the shell and in `.env`, was written first and failed.
- C1 P3 (1): `trust-dsn-init` removes the DSN file of a tenant that left the list and stops on any entry that is not a tenant file (tests written first; Linux-only, so CI is their proof). P3 (2): noted in the README (a 600 secret needs another mount). P3 (3): the stand-in tenant must be in the list (refused otherwise). P3 (4) and (5): the body and the README are updated; `test_secrets_local.sh` is run by CI (my host cannot run it in full).
- EL: the body is brought to the current head and says SR should read the delta since `fc50470d`.

# Session 2026-11-20 · ENV-04 · the independent reads of noema#730 at `527fa32e`, and the merge (IL)
- Merged `origin/main` once (conflict only in this file; both sides kept; main now has noema#821, so BO's writers are in).
- C1 P3 (3), EL: `--no-digit-rule` kept the Luhn-valid card-number rule on (a run id has 11 digits and cannot be a card of 13 to 19); it turns off only the generic digit-run and the ten-digit account rules. The probe now plants and requires `card_number` in that mode too. Tests first (red): scanner, probe, wrapper mutants for the new kind, and a card number in the public job log.
- C1 P3 (2): the standing `"run_id"` allow pattern (any 20 digits) is gone (14 patterns again); the scan step builds this run's allow file (the standing one plus one line that names `$GITHUB_RUN_ID` exactly), so only this run's own id passes under that key. Tests first: the exact id passes, a different 11-digit id, a 20-digit string and a doubled id are found.
- C1 P3 (1): the `count` step's comment no longer claims a control read per case (that is the `access` step's check). EL: `READINESS_REQUIRED` has an owner (IL, noema#436) and its flip is the final composition PR's (A445).
- Not changed: the task id (`ENV-04` is the G1 stack workflow's name in this repository's history; the plan row of that id is different: the Steward can re-point it).

# Session 2026-11-20 · ENV-04 · A477 and the lower-case tenant list on noema#790 (IL)
- A477 (docs#213, interim): the gateway, worker and recovery bench values are in the bench core's settings and in netcheck's table by number (`NOEMA_INPUT_DEDUP_WINDOW` 5m, `NOEMA_INPUT_RECORD_TIMEOUT` 5s, queue 64, workers 1, run timeout 180000, start timeout 30000, `NOEMA_EDGE_KEY_MAX_ACTIVE` 2, activity timeout 240000, drain 30000, recovery idle 240000, batch 50). `settings_order_problems` checks the ordering the loaders enforce (activity and recovery idle each above the run timeout); 141 mutations. A475's lifetimes (grant 60 s, record 60 s, transaction 10 s) are not added: their setting names are not yet on `main`.
- Item 6 (C1's P2): every word of the tenant list must match `^[a-z0-9_]+$` before the tenant shape is checked, so an upper-case word is refused. My earlier hostile-value tests were weaker than they looked (a single-word list also failed for lacking the stand-in tenant); each hostile value is now tried alone and beside the stand-in tenant, and `ten_AB9` failed first.
- Not added (names or shapes not final): the core-edge key seed, the input hash key and the edge DSN one-shot (A469 to A485 item 3: file-mounted, owner-only, mode 0400; EL states a loader's different mode in its PR), and the migrate and tenant-init one-shots (noema#846 is ready; noema#852 is a draft with two open decisions).

# Session 2026-11-21 · ENV-04 · the core restart and the re-read phases in the stack job (A476, A502; IL)
- A476 (corrected by A502): the recovery trigger is a restart of the core container (the core runs one recovery sweep at startup). Three full-mode steps between the evidence and the package: `restart` (`docker compose restart core`), `recovery` and `replay` (BO's `reread --phase after_recovery` and `--phase after_replay`, noema#854: each waits for `/readyz` 200 with a 120 s bound, reads every case's reconstruction through the audit-only route as an audit session of the run's own IdP, and `after_replay` requires the bytes to equal the first read). The Result step fails the run unless each exits 0. No credential file and no seeded principal (A502). The 120 s bound is a run setting I chose, not a ruling: BO may change it with a one-line test change.
- Depends on noema#854 merging first: until `benchmark.runner.reread` is on `main`, a full run fails at `recovery` with NOT RUN. The stack-check mode does not run these steps.
- Not verified: the workflow itself (no Actions or Docker here); `docker compose restart` on the runner and the core's recovery sweep at startup (EL's, noema#789 merged).

# Session 2026-11-22 · ENV-04 · noema#876 (IL)
- The two re-read steps take the five IdP flags from `benchmark/out/idp/runner-args` (written by `make bench-tokens`; host paths mapped to `/bench-out`), never `--tokens-dir` (A517). `--artifact-files` belongs to BO's package renderer (noema#875, not on main): the stamped list is added when #875 merges.

# Session 2026-11-21 · ENV-04 · the A506 rows for the final composition (IL)
- A506 (docs#222, interim): every row the core reads at the heads of noema#838, main and noema#847 is in the bench core's settings and in netcheck's table by value (paths, the two key max-active values, the Temporal namespace, task queue and CA file, the escalation graph, the Recorder's lifetimes), with the grant-not-above-record ordering. `trust-dsn-init` now has one job per DSN directory (four; the edge login uses its own per-login secret); a new one-shot `core-keys-init` writes the two signing-key files and the input hash key (64 hex, 0400, owner 65532); `secrets_local.sh` makes the three values. Netcheck: every DSN and key volume has one writer and the core as its only (read-only) reader, the key values are held by `core-keys-init` only, the emitter secrets by postgres and `trust-dsn-init` only, the Temporal client certificate and key and `NOEMA_WRITE_SWAP_DECISIONS` are never set. 157 mutations (16 new), all caught.
- Values that are EL's proposals, not rulings: the Recorder lifetimes (noema#847's body; the Founder confirms). Not added: `NOEMA_TRUST_AT_RISK_BPS` (the pack's), anything test-only.
- Not verified: the Linux-only tests (`trust-dsn/test_init.py` 14, `core-keys/test_init.py` 7, `test_secrets_local.sh`) and the stack; no Docker daemon here. Whether the core loads what the init writes needs noema#838 on `main` (noema#692).

# Session 2026-11-22 · ENV-04 · noema#874 reopened as a clean branch (A429, A509; IL)
- noema#874's `checks` job failed on gitleaks (1 leak in my commit): the test literal `EDGE_PASSWORD = "<24 hex characters>"` (a high-entropy value on a name that says PASSWORD). A429 says a PR whose history carries a key-shaped test string is closed and reopened from a fresh branch with the clean tree, with no ignore line and no rewrite; so #874 is closed and this branch carries one commit with a readable placeholder (`synthetic-edge-login-secret`, which still meets the 16-character rule).
- Codex P1 (the key initializer cannot read secrets that keep the host user's ownership and mode 0600 once every capability is dropped): `core-keys-init` adds `DAC_READ_SEARCH` (reads any file, writes none; never `DAC_OVERRIDE`), netcheck requires exactly that set and has two new mutations (159). Not testable without root and a differently-owned source (CI runs as a non-root user), so the netcheck rule and the first stack-check are the proof; `trust-dsn-init` is unchanged (its secrets are mode 644).
- A509: the DSN files are mode 0400 owned by uid 65532 (the core's uid): unchanged, tested in `trust-dsn/test_init.py`.

## Session 2026-11-23 (fifth) · ENV-04: lane 5 mounts (A554, A564)
- Read C1's card, `status/LANE-5.md` on noema#917, and A554 and A564. Added to the bench stack: `execution_dsns` and `outcome_dsns` (the `execution_rw__<t>` and `outcome_rw__<t>` logins, written by `trust-dsn-init` with the shared tenant password) and `execution_keys` and `outcome_keys` (`core-execution.ed25519.1.key`, `core-outcome.ed25519.1.key`, written by `core-keys-init` from two new seeds `secrets_local.sh` makes), each 0400, owner 65532, read-only in the core and nowhere else; the six settings `NOEMA_EXECUTION_DSN_DIR`, `NOEMA_OUTCOME_DSN_DIR`, `NOEMA_EXECUTION_KEY_DIR`, `NOEMA_EXECUTION_KEY_MAX_ACTIVE`, `NOEMA_OUTCOME_KEY_DIR`, `NOEMA_OUTCOME_KEY_MAX_ACTIVE`. netcheck pins every volume, secret holder and value (178 mutations in all, every one caught).
- Open (for the Architect): the two `MAX_ACTIVE` values. No ruling gives them; I used 2, the overlap of the orchestrator's and the edge's keys (A506). C1's card names the setting but no number.
- The card's own flag, repeated: the Outcome verdict signer has a directory (A564) but no slice-3 consumer yet.
- A572: the narrower provisioning login (debt entry above) is debt for C2 with IL, due before the G2 review.

## Session 2026-11-23 (sixth) · ENV-04: BO's row 12 inputs and one source for the IdP flags
- The scan writes BO's stamped inventory (`--inventory`, `--stamp`, `--root benchmark/out`); a new step lists the files the artifact holds (stamped `artifact-files.txt`, hidden files included, the scanner reads them) and compares it with the inventory (`files_match`, failing the Result step on a difference). The scan output, the inventory and the file list travel in a second artifact, `bench-g1-stack-evidence-<run id>`, because they are written by or after the scan and so cannot be in the scanned set; the first artifact is exactly the scanned files (the summary is now uploaded too, as it is scanned).
- The five IdP flags come from `benchmark/out/idp/runner-args` (A517) in the run and the probes as well as the two re-reads, so a changed key id, audience or tenant reaches every step. Checked against the real scanner on a temporary tree: inventory and file list agree (`files_match=0`).
- Found by that check, not by a unit test: the scanner reads hidden files and `upload-artifact` skips them by default, which would have made the two sets differ; the artifact now sets `include-hidden-files: true`.

# Session 2026-11-23 · ENV-04

## Session summary
- What changed: the bench stack gains `operator-dsn-init`, `core-trust-keys-init`, `tenant-init` and `policy-publish` (A445, A484, A489, A527). `tenant-init` runs `cmd/tenantinit` with the superuser's DSN file (a login that may call `key_register`; the migration login's password stays with the migrate one-shot and postgres alone) and no `--tenant`. `policy-publish` runs `cmd/policypublish` for the bench tenant after it; the core waits for it. The core image now also builds `cmd/tenantinit`. netcheck pins every holder, volume reader and flag with 46 new mutations (205 in all, all caught).
- Why: the Trust intake needs a registered core-trust key and a policy in force before the stack serves.
- Risks, and how they are contained: the superuser password gains one holder (`operator-dsn-init`, no network); netcheck refuses any other. Not run on a live stack (no Docker daemon here, `docker compose config` only).
- Tests added: `deploy/compose/operator-dsn/test_init.py` (6, seen failing first); netcheck mutations.
- A522: `core-keys-init` is accepted by the first stack-check run, as is `core-trust-keys-init` (same script).
- Debt entries: none. Conflicts raised: none.
- Not done: the migrate one-shot and its A527 netcheck rule wait for C2's A521 flags (no PR on origin). #889 was closed because #880 (the 4096 reader) had not landed (Codex P2); reopen the two-line change after #880 merges.

## Pre-work for the next agent
- Exact next step: read the first stack-check run for `operator-dsn-init`, `core-trust-keys-init`, `tenant-init` (exit 0, `{"keys_registered":1,...}`) and `policy-publish` (`published`).
- Open questions: tenant-init's superuser login is my reading (A445 to A489 name no login); the Architect should confirm. The migrate one-shot flags (C2).
- Do not touch: the migrator password's holders (postgres, migrate).

## Session 2026-11-23 (second) · ENV-04
- Added to the same PR (one more commit): `migrator-password-init` and `migrate` (A484, A521, A527; EL's flags: host, port, database, user, password file mode 0400 owner 65532 one line, `--sslmode` of `disable`, `prefer` or `require`, default `prefer`). The init step is the migration password's second holder after postgres (the Founder's choice); netcheck pins the holders, the volume's reader and every flag, with a mutation for each leak path (argument, environment, a second reader or holder): 237 mutations, all caught. `tenant-init` now waits for `migrate`.
- Must merge AFTER C2's `cmd/migrate` change (feat/EXE-08-migrate-password-file-v2 is red tests only at e18460d9): with the old `--dsn-file` binary the migrate service fails.
- Gap I did not close: nothing in Compose runs `create-tenants.sh` (superuser) or `provision-tenants.sh` (migration login) between `migrate` and `tenant-init`/`policy-publish`; policy-publish needs the tenant row and the tenant's logins. Whoever owns those two steps must be named (the second needs the migration password, another holder under A527).

## Session 2026-11-23 (third) · ENV-04
- The tenant steps are in the same PR (#894): `tenants-create` (create-tenants.sh as the superuser) and `tenants-provision` (provision-tenants.sh as the migration login), from the database image, as 65532, with a one-line libpq password file each (`pgpass`, written by the two init steps beside their other file). Order: migrate, tenants-create, tenants-provision, tenant-init, policy-publish, core. netcheck: 261 mutations, all caught.
- Reading to confirm (Architect): `tenants-provision` is a second reader of the migration password's volume besides `migrate`, which A527 as I was given it does not name. The alternative (run provision-tenants.sh as the superuser) leaves the script's tested path.
- Not run on a live stack: libpq as a uid with no passwd entry, `/bin/sh` heredocs on a read-only root with a tmpfs /tmp.

## Session 2026-11-23 (fourth) · ENV-04
- A536 (amends A527): migrate and tenants-provision are the migration password's two holders besides postgres, each a hardened one-shot, pinned in netcheck. Replaced the `migrator-password-init` step by a private copy of the password (`migrator_password_file`, mode 0400, host user; `secrets_local.sh` makes it, tested) that both one-shots hold and read as the host user, because cmd/migrate refuses a password file that is not 0400 and owned by its own user. `tenants-provision` builds libpq's password file in its tmpfs from that copy. netcheck: 270 mutations, all caught, a mutation per leak path.
- Fixed the red `compose-scripts` on #894: `test_bench_tenants.py` counted the tenant-list expression once in the overlay and now there are three (postgres and the two tenant one-shots).
- A540: the statements `provision-tenants.sh` runs are listed in the PR body; it needs no DDL.
- Not verified without a Docker daemon: that the host user can read the 0400 copy inside the container (same uid), libpq as a uid with no passwd entry, and the tmpfs heredocs on a read-only root. The first stack run decides.

## Debt entry, 2026-11-23 · ENV-04 (A540; register number to be assigned by the Steward)
- **Shortcut:** `tenants-provision` runs `provision-tenants.sh` as the migration login `noema_migration_login`, so a second one-shot holds the migration password (A536) although the script needs far less than that login can do.
- **What the script runs** (listed in noema#894): per tenant and power group, one `SELECT ... provision_tenant_login(...)`; per tenant and emitter, one `SELECT ... provision_emitter_login(...)`. No DDL, no `CREATE`, `ALTER`, `GRANT` or `DROP`, no role creation. The caller needs only `USAGE` on schema `public`, `SELECT` on `tenant_login`, and `EXECUTE` on the two functions.
- **Why it stays for now:** only the migration login holds `EXECUTE` on those functions (migrations 0006 and 0011 grant it `TO CURRENT_USER`, the migrating role). A narrower login needs (1) a role and a secret in `deploy/compose/postgres/init.sh`, which runs before the functions exist and so cannot grant, and (2) a new migration granting `EXECUTE` on both functions and `SELECT` on `tenant_login` to it. The migration is C2's lane.
- **Payoff when repaid:** `tenants-provision` stops holding the migration password; the password has one holder besides postgres, `migrate`.
- **Owner:** C2 (the migration) with IL (init.sh role, secret, Compose). **Date:** none given by A540 (not a blocker); the Architect sets it. **Evidence that invalidates it:** a need for DDL in `provision-tenants.sh`, which would keep the migration login.

# Session 2026-11-24 · ENV-04 · the first stack-check on main after #924 and #921 (IL)
- Run 45423718092 on main at 76c0e3a1 (stack-check, body limit 32768). **Live proof of A536 and the tenant steps:** `migrate`, `tenants-create`, `tenants-provision`, `operator-dsn-init`, `core-trust-keys-init`, `tenant-init`, `policy-publish`, `trust-dsn-init` (six directories), `grant-keys-init` and `core-keys-init` all exited 0 on the runner; postgres, temporal, model, mock-case, standin and the hostility proxy were healthy. `joblog-scan` passed.
- **The core exited 1 at start** (`dependency failed to start: container noema-local-core-1 exited (1)`), so no `/readyz` body exists. The cause is in the core's own log, which the run did not show: the start had failed before the run step, the scan was told to read `mock-case.log` and `mock-case-collector.err`, found neither and refused (exit 2), so the service logs were not scanned, not uploaded and not shown. Row 12 failed closed as designed (`files_match=1`, no inventory); it is not yet proven live.
- Fix in this change: the capture step, which runs on every outcome, leaves an empty file where the run step made none (an existing collector file is never touched); the job still fails on the start step. Test red first: `test_a_start_that_failed_before_the_run_step_still_leaves_both_collector_files_for_the_scan`.
- The names of the six settings the core reads for the Execution side match Compose (checked by grep: `NOEMA_EXECUTION_DSN_DIR`, `NOEMA_OUTCOME_DSN_DIR`, `NOEMA_EXECUTION_KEY_DIR`, `NOEMA_EXECUTION_KEY_MAX_ACTIVE`; the two Outcome key settings are not read yet), so I do not claim lane 5 is the cause; it is unknown until the log is read.
