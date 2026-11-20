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
