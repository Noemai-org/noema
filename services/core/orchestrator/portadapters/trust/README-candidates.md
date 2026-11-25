# portadapters/trust: the Candidates adapter (T7)

Date: 2026-11-18 · Owner: Core Engineer 2 (C2) · Task: TRU-04 (closest row; branch TRU-07-intake-candidates) · Status: built, not yet merged (noema#741) · All data is SYNTHETIC.

**What it does.** `trustport.Candidates` implements `intake.Candidates`, the T7 read of the Trust intake (PERD 23.3, ruling A329). For an intent whose reference slot is not resolved it reads the candidates through the principal-scoped candidates read (`candidates-request.v1` and `candidates-response.v1`, REQ-CTX-006) as the session's tenant and principal, has them scored and labelled by two ports, and answers them with the pack's disambiguation data. It decides nothing: Trust sorts, floors and asks (INV-01).

**REQs.** REQ-CTX-006, REQ-AMB-002, REQ-AMB-003, REQ-AMB-004, REQ-TRU-002, REQ-TRU-003, REQ-NFR-002. **SoT.** INV-01, INV-11, INV-12, INV-14, INV-16, INV-21, AGENT-13, AGENT-16.

## Parts (all passed to `NewCandidates`; none has a default)

| Part | What it is | Real one |
|---|---|---|
| `Intents` | the stored intent, as the adapter's tenant | `intentstore.Reader` |
| `Reader` | the candidates read, in the contract's types | not built: `evidence/candidates_api` is empty (`doc.go` only); the orchestrator area may not import `adapters` |
| `Ranker` | one score per candidate, basis points | not built: the ranking of REQ-AMB-003 is Python, in the model service |
| `Labeller` | `Display` and the askable attributes, from the pack's templates | not built (EL's); the pack now declares the options key, the display template and the attribute order on the investigate node (A555, noema#916) |
| `Nodes` | the pack's data by intent node: slot names, resource kind, raw bound, tolerances, `intake.Disambiguation` (floor in basis points) | the composition fills it from the pack |

Until `Reader`, `Ranker` and `Labeller` exist the adapter cannot be wired into `cmd/core`.

## Rules, and what refuses

| Rule | Refusal |
|---|---|
| zero value, a missing part, pack data below a platform ceiling or floor, a bound over the contract's | `ErrConfig` |
| the context has a deadline | `ErrCandidatesDeadline` |
| tenant and principal are set and of the contract's shape (INV-12) | `ErrSession` |
| the session's tenant is the adapter's (INV-11) | `ErrCandidatesTenant` |
| the admitted intent, and the stored one, are the session's tenant and principal, and agree on ID, version, node and correlation; the document names exactly the session's principal | `ErrCandidatesIntent` (one answer for not found, not owned, store failure and unusable document, REQ-TRU-003) |
| the node has candidate data in the pack and its reference slot is not resolved | `ErrCandidatesNotNeeded` |
| the intent gives a time window of two UTC instants | `ErrCandidatesNoWindow`: nothing is read (REQ-AMB-002) |
| a slot that is not what its type says (an amount that is not a whole positive number, text that is too long) | `ErrCandidatesIntent` |
| the read fails | `ErrCandidatesRead`, the cause behind it |
| the answer is for this tenant and correlation, within the pack's raw bound, each candidate of the contract's shape and the pack's resource kind, none twice | `ErrBadAnswer`, the whole answer |
| one score per candidate, 0 to 10000; a label not empty, within the pack's digit run, with only listed attributes, never the resource's reference or the candidate id | `ErrBadAnswer`; a port that fails is `ErrCandidatesRank` |

**The search.** The request is the session's tenant and principal, the stored intent's correlation, the pack's resource kind, the intent's window widened by the pack's time tolerance, and, when the intent has an amount in a contract currency, the amount widened outward (rounded up) by the pack's tolerance. The customer's words are never in the query (REQ-CTX-007); only the ranker sees them, bounded to 64 characters.

**Error text.** Fixed. It never carries a row, another principal's ID, a word of the customer or the cause's text (AGENT-13); the cause is reachable with `errors.Is`. An error is never an empty list; an empty valid answer returns no candidates with the pack's data, and the intake turns it into ESCALATE.

**Names.** Everything is prefixed `Candidate` or `ErrCandidates` so that the other adapters of this package (Evidence, Reconciler, A329) do not collide.

## Not here

No Record event (the intake records the decision). No ranking, no labelling, no pack reading. No DB test: the only database component is `intentstore.Reader`, tested in its package.

## Tests

`go test -race ./orchestrator/portadapters/trust/` from `services/core/`. No database. The test world is a candidate reader that scopes its rows by the request's tenant and principal; its faults are a context that really expires and answers that really are out of contract.
