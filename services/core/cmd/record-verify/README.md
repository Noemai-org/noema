# cmd/record-verify

Date: 2026-11-17 · Status: built by REC-06 (draft until the checks are green at the final head)

The chain verifier job. CI runs it on every merge against the CI database, and the nightly workflow runs it again. It is one small command over the REC-05 verifier (`record/verify`): the same verifier code, the same canonical serialisation (RFC 8785, in `record/chain`), and the restricted login of OD-06 (DEC-2026-10-17). The Record is tamper-evident within the POC threat model, never immutable.

**Implements:** REQ-AUD-001 (the REQ the REC-06 card cites); uses REQ-AUD-005 and REQ-AUD-006 as built by REC-05 and REC-08. SoT IDs: INV-11, POC-FR-12.

## What it does

- Reads the connection string of the verification login from `NOEMA_RECORD_VERIFY_DATABASE_URL` and nothing else. The value is a credential: it is never printed and never put in an error.
- Refuses to run as any other login (`preflight`): the login must be a member of `record_verify`, must not be a superuser and must hold no INSERT, UPDATE, DELETE or TRUNCATE on `record_event` or `record_chain_head`. The administrator, a power's tenant login and a verification login that was given one write grant are each refused (exit 2) before anything is read.
- Runs `verify.Verifier.Run` under a 10 minute deadline (an operating limit of this command, not a safety threshold). The verifier recomputes every hash, projection column and payload hash, checks every signature and the chain head, and on a break counts it, turns the tenant's kill switch on and raises the alert (REC-05).
- Prints JSON lines to stdout: one `RECORD_CHAIN_BREAK` per finding (`tenant_id`, `seq`, `kind`) and a last `RECORD_VERIFY_RESULT` (`result`, `run_at`, `tenants`, `events`, `breaks`, `noema_record_chain_break_total` by kind). It never prints a finding's description, an error's text, event content or the connection string (AGENT-13); a database error adds only its five-character SQLSTATE to stderr.

| Exit | Meaning |
|---|---|
| 0 | every tenant verified, and there was at least one |
| 1 | a chain break was found (wins over anything unreadable in the same run) |
| 2 | the job could not verify: no URL, a refused login, an unreachable database, an unreadable tenant, an alert that could not be written, or no tenant at all. Unknown is never a pass |

## How CI and the nightly job use it

The tests named `TestRecordVerifyCI*` (`ci_test.go`) build a SYNTHETIC Record in a throwaway database with the real appender and signers, then run this command as its own process as the verification login. The clean Record must exit 0; each planted tampering must exit 1: an edited row, a stored hash edited, a payload edited after append, a broken prior hash, a missing sequence number, a reordered pair, a dropped tail. The core workflow runs them from shard 0 on every merge (`core.yml`, step "Chain verifier job on the CI database"); `record-verify-nightly.yml` runs the same command on a schedule against a freshly built database of the same kind. Both check the test output for the result lines, so a renamed test cannot make the step pass by matching nothing.

## Run it

```
cd services/core && go test -race -count=1 ./cmd/record-verify        # unit tests (no database) and the CI-database tests
NOEMA_RECORD_VERIFY_DATABASE_URL=... go run ./cmd/record-verify        # against a database, as its verification login
```

Without the pgsodium extension no event can be appended (the Record trigger fails closed), so the tests that build a Record fail on a machine without it and run in CI; the tests that need no events run anywhere.

## What it does not do

- It does not verify a persistent database yet: GitHub's runners have none, so the nightly run verifies a database it builds. Pointing the command at an environment's database is a change of the one variable once such a database and its verification login exist.
- It does not file an issue or page anyone: a failed run is the alert (REQ-AUD-005's security alert, S1 incident and routing are open on REC-06; see `status/REC-06.md`).
- It does not check the chain head against an exported checkpoint (REC-07) and does not replay decisions (REQ-AUD-005, a later slice).
- It never repairs a chain.
