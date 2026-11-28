# Status · INP-01 slice 2 · the open investigation of a follow-up (Core Engineer 2), STACKED ON noema#1053

Updated 2026-11-28. All data is SYNTHETIC. PR noema#1075 (base: the #1053 branch).

## Built
`understanding/resume.Resolver.Open(ctx, session, actor, conversationRef)`: the order, the one answer for every reference that is not the session's own, the security event for another principal's investigation and the ended-investigation refusal are in the package README. Ports: `Reader` (`intentstore.Reader` implements `Latest`), `States` (an investigation has ended), `Denials` (the `SECURITY_EVENT`).
- **Red first:** `4bdfb1584` (stub package and tests; local red in the PR comment). **Hand mutants:** 11 killed (an invalid session allowed; tenant or chain unchecked; principal unchecked; the record failure swallowed; the actor dropped; the ended state ignored; a read error hidden as not found; a state error read as open; the chain id wrong; the denial port not required).

## Not done (the rest of INP-01)
| Slice | What | Owner |
|---|---|---|
| 4 | `INVESTIGATION_RESUMED` recorded before the follow-up is saved (Record first) | C2 (stacked on slice 3) |
| 6 | the `States` and `Denials` implementations over the Record, the call from the edge's conversation route (`cmd/core/conversations.go`), PUS-08 end to end, US-11 and the concurrent follow-up on real Postgres | C2 with EL's read |

## Pre-work for the next agent
- Exact next step: slice 4, then slice 6 once #1053, slice 3 and this have merged.
- Do not touch: `intentstore` (the store's key is the optimistic lock).
