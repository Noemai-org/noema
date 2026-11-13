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
