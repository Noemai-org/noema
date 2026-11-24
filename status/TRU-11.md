# Status · TRU-11 (Core Engineer 1) · the per-principal rate limit at T11

Updated 2026-11-24. Plan task TRU-11: owner C1; REQ-TRU-012; Needs TRU-02 (Done); done when the descriptor's `limits.rate_per_principal_per_hour` is enforced as a policy-layer check (REJECT `RATE_LIMITED`, no write). Ruling A630 (interim, relayed): count as C1 proposed, migration, Tier 1, independent read.

## State: draft pull request noema#952 (red pushed, green local until the red CI job is read). TRU-11 is not Done until merged evidence.

## Session 2026-11-24 · TRU-11

### Session summary
- What changed (PR links): noema#952. `trust/intake` (port `Rates`, the check in T11), `orchestrator/portadapters/trust/rates.go` (the count), `trustpack.Pack.RatePerPrincipalPerHour` and `registry.Registry.RatePerPrincipalPerHour` (the limit, from the descriptor), migration `0041_grant_principal_rate_index.sql` (one index).
- Why: REQ-TRU-012. The requirement names the check but not where attempts are counted; A630 takes the AGENT-02 default: the principal's grants for the capability in the last hour on the database clock.
- Risks, and how they are contained: (1) **not composed in `cmd/core`**: no deployment limits anything until a composition root passes the adapter; composing it changes G1 behaviour, and A622 keeps G2 work off the G1 path (Architect/EL to say when). (2) Every grant counts whatever its status, and every capability of the authorized set is checked: both the stricter reading, open for the Architect. (3) The tenant-wide limit of the task title has no descriptor field and no ruling: not built. (4) The replay of a decision gives the same REJECT only while the count is the same; the decision records the reason, not the count.
- Tests added (REQ-TRU-012, REQ-TRU-004, INV-12, REQ-NFR-002): `trust/intake/rate_limited_test.go` (over the rate, any capability of the set, under the rate and what is asked, a read never asks, no port, error and panic fail closed); `portadapters/trust/rates_test.go` (real PostgreSQL: the limit, the hour, another principal, capability and tenant, an undeclared limit, fail closed, config); `portadapters/trustpack/rate_limit_test.go` (the limit read from the pack file); `platform/db/principal_rate_index_test.go` (the named index).
- Test defect found at the green: the red expected the capabilities in the order create_case then adjust_record; the set is sorted, so adjust_record comes first. The expectation was fixed, no assertion weakened.
- Regression cases added: the tests above. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: after the G1 run, wire `trustport.NewRates(pools, pack, timeout)` into `cmd/core` (compose_trustinputs.go) as `Service.Rates` (optional, not a `Ports` field: cmd/core wires every `Ports` field and refuses to start without it); the timeout is the one of the other trust adapters (AGENT-16).
- Open questions (Architect): the three choices in the risks above; the tenant-wide limit.
- Do not touch: the Writer's grant insert and the 0001/0012/0014 guards of `decision_grant`.
