# trust/intakeadapters

The production adapters of two ports of the Trust intake (`trust/intake`, ports_t10_t12.go): `Policies` (T11) and `Clocks` (T12). The ports are defined by their consumer; this package implements them over `trust/policy` (the engine), `trust/policystore` (the publication store), `trust/clocks` and the pack's data. It decides nothing the engines do not decide, and holds no threshold, TTL, at-risk fraction, routing rule or industry term (INV-14, REQ-OPS-003).

**Implements:** REQ-TRU-004, REQ-TRU-005, REQ-TRU-006, REQ-TRU-007 (production adapters of T11 and T12); A306 (one evaluation for each capability of the authorized set, answered for that capability alone). **Never:** read a clock (the query carries the reading), order policy versions itself (the database does), read the intent store, the evidence layer or an enterprise source (those are the ports `Admissions`, `FactSource` and `Anchors`, supplied by the composition root), treat an absence as a negative fact (INV-19).

Status of each part, and what is not built and why: `status/TRU-adapters-policies-clocks.md`.

## Policies

- `LoadPolicyBook(currentPack, olderPacks...)`: the pack's `policy` document(s) as data, checked by `policy.Parse` against the pack's own vocabulary (descriptors, variables, capabilities). One policy per book; every pack must carry the same policy ID and the same vocabulary; each version once. A rule that names anything but a capability is refused at load (an intent node is not a target here). For each target the book knows the variables and the entities its rules read (`FactNeed`).
- `NewPolicies(PoliciesConfig{Book, Publications, Admissions, Facts})`, then `Evaluate(ctx, PolicyQuery)`:
  1. the query is refused (`ErrQuery`) before any port is asked if it lacks a tenant, an intent, a version, a reading or a target (the capability, or the intent node when the query names none);
  2. the admission time of the evaluated intent version is read from `Admissions` and must be at least 1 and not after the reading;
  3. the tenant's publications of the policy are read (`StorePublications`, as the tenant's own Trust login: `policystore.Published`); the version in force at the reading is the newest published at or before it (ACTIVE), the others RETIRED, each effective from its publication; an AT_ADMISSION rule is read from the version in force at admission (D8); the newest version and the one in force at admission must have a document in the book (`ErrNoDocument`), an older one nobody needs does not; times that do not follow the database's order of versions are `ErrPublications`;
  4. the facts the rules read are asked of `FactSource` (only what `FactNeed` names) and checked: all needed present, nothing undeclared, each of its declared type, enum values declared (`ErrFacts`);
  5. the real engine evaluates; a rule that errors is `policy.ErrCondition` and no answer; no matching PERMIT is the engine's REJECT `NO_PERMITTING_RULE`;
  6. the answer carries every rule that matched with its policy and version (INV-09), the versions used (REQ-TRU-007), `PermitMatched`, and for a WAIT `ReevaluateAt` = the reading plus the winning rule's `wait_seconds` (a whole number of at least 1 that fits the clock, else `ErrRule`).
- A tenant with no publication, or a reading before the first one, is REJECT `NO_PERMITTING_RULE` with no rule and no version. A store that cannot be read is an error, never that.

## Clocks

- `LoadClockBook(pack)`: the pack's `clocks`, `trust_profiles[].clocks` and `intent_nodes[].trust_profile`, checked at load (`ErrPack`).
- `NewClocks(ClocksConfig{Book, Anchors, AtRisk, Routes})`: `AtRisk` and `Routes` are data the composition root passes from the pack; the adapter refuses an unusable fraction and a route that is not a phase to a pack reason code, and defaults neither. **The pack has no such data today** (status file).
- `Applicable`: the clocks of the node's trust profile, in the profile's order, each with `Anchor` nil when the event has not happened (NOT_STARTED, never assumed) and a pointer of its own when it has; an unknown node is `ErrQuery` and not "no clock"; a node with no clock gets an empty set and needs no snapshot; an anchor the source cannot give is an error and never NOT_STARTED.
- `Route`: the most severe phase among the statuses that the routes name escalates with that route's reason code; a phase not routed does not. A breached clock the pack does not route is not escalated here: the intake does it (`CLOCK_BREACHED_UNROUTED`, A225).

## Tests

`cd services/core && go test ./trust/intakeadapters/ ./trust/policystore/`. The shipped `packs/fintech_v1.json`, the real policy engine and `clocks.Evaluate`, and a real PostgreSQL (`internal/testpg`) read as each tenant's own Trust login run; the admission time, the facts and the anchors are the tests' own (no production implementation exists). Synthetic data. Every test names the REQ, INV or A-ruling it proves.
