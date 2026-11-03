# Status · core-engineer-2

Written only by the core-engineer-2 role (see `README.md`). Append dated entries, newest last. History before 31 Oct 2026 is in `status/<task-id>.md`.

## Entries

### 2026-11-03 · OUT-01 (local branch `feat/out-01-outcome-verifier`, no PR yet)

- **Completed (local; no check-run link exists because no PR can be opened):** migration 0004 and the verify-by-key verifier, store and end-to-end tests. `go test -race ./...` from `services/core` was green except two `platform/db` tests that my migration affected; both are resolved (below). The architecture lint is OK.
- **Blocked:** opening the PR. The `noemai-systems` account has four open `noema` PRs (#104, #107, #108, #109); the limit is four (plan section 5). Awaiting a free slot or a documented exception.
- **Decisions taken (to be recorded in the PR body):**
  - Read retries: the first read plus three retries after 1, 3 and 9 s of simulated time (PERD 29.2 "3 attempts with backoff of 1, 3 and 9 s"). The values are supplied by the caller as configuration (the lint forbids literals in `outcome/`; OPS-01 owns the defaults).
  - A key present with a true predicate is ACHIEVED; a true predicate with no case under the key is UNKNOWN, not a success. A complete empty page by key is a verified absence (NOT_ACHIEVED) for a QUERYABLE_BY_KEY capability; for any other capability the verdict is UNKNOWN.
  - An unbound predicate variable is UNKNOWN: the language reads it as null and a comparison with null as false, which would give a false NOT_ACHIEVED.
  - The transaction read is scoped to the principal the grant was issued to (the case adapter requires it; INV-12).
  - The Record events are appended by the Go side in the same transaction as `outcome_record_verdict` (the hash is computed outside the database); the card's wording "only through the function" holds for the three tables, not for the Record.
  - `execution_rw`'s privileges on `recovery_request` are unchanged: a guard trigger and the unique `verdict_id` already make a forged verdict-sourced request impossible.
- **Escalations:** (1) The card's `outcome/e2e` location cannot hold the end-to-end test: `import-boundary` and `import-adapters` apply to test files. The tests are in `adapters/outcome_e2e`. (2) `verifier/doc.go` listed `adapters` as importable; the lint forbids it. The verifier defines its own `CaseReader` port. (3) `platform/db/schema_test.go` changed: `execution_receipt` is now referenced by `outcome_verdict`, so `TRUNCATE` fails with 0A000 before the trigger; the test asserts the trigger exists instead of its error code.
- **Debt to file (docs repository):** the `outcome_rw` credential can call the write path until REC-08; `mockcase_test.go` is a copy of the harness in `execution/e2e`; no production wiring supplies the read budget (OPS-01); no trigger calls `Verify` from `outcome.verify`; duplicate delivery of `outcome.verify` is not deduplicated (one verdict per call).
- **Next:** open the PR when a slot is free; UND-12 (docs#82 approved) next.
