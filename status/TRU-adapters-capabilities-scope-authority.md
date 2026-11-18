# Status · TRU-02 production adapters · Capabilities, Scope, Authority and the escalation set (Core Engineer 1, prepared locally)

Updated 2026-11-18. The user's order of 18 Nov (relayed, interim, not committed: rulings A329 to A340, A335 for the escalation set; A313 and A319 are committed in `docs`, which the local `docs` checkout does not yet contain, so they were read from the order). Follows noema#572 and #710. Tier 1 (trust, P0). No task ID was given: the commits carry TRU-02 (the intake's gate order, whose ports these are); the Repo Steward may re-point.

## State: four local commits (red `fb610ba3`, green `d074a844`, mutant-gap tests `1ae1d342`, docs) on `prep/adapters-capabilities-scope-authority`; not pushed, no PR

# Session 2026-11-18 · TRU-02 adapters

## Session summary
- What changed (PR links): none yet. New package `services/core/orchestrator/portadapters/trustpack` (sibling of `portadapters/trust`, which is EL's and untouched): `LoadPack`, `NewCapabilities`, `NewScope`, `NewAuthority`, `EscalationSetFromPack`, the `Remedies` port. No edit to `trust/**`, `cmd/core`, `execution/**` or any pack file.
- Why: the intake's `Capabilities`, `Scope` and `Authority` ports and `recorder.Writer.Escalation` had no production adapter, and the pack (data) holds what they need (descriptors, trust profiles, action graphs).
- Risks, and how they are contained: see Decisions. The adapters only read pack data loaded once and refuse (error, false, empty set) on anything missing or unsound; none adds an allow path. `Scope` is pack-wide, not per tenant (Decision 3).
- Tests added (REQ-TRU-004, REQ-TRU-005, REQ-TRU-010, REQ-CAP-001, REQ-CAP-002, REQ-AG-001; INV-05, INV-14, INV-16, INV-18, INV-25, BD-08, A255, A306): 40 test functions in 4 test files plus two helper files, on the real `packs/fintech_v1.json` and its three graph files with expectations read independently by the test; a synthetic tenant; changed copies of the pack via `testing/fstest` for 20 refusals; a scan that no pack identifier is written in the adapter code (with a planted literal to show the scan can fail).
- Red first at `fb610ba3`: stubs answering "not built"; 38 tests failed (one passed: the scan, which holds on the stubs). The failures are "not built", not assertion detail; the refusal tests name their sentinel with `errors.Is`, so a stub's error does not satisfy them.
- Mutants (44), all killed, source restored by `git checkout -- <file>` after each, `git diff` empty before each commit: P1 prohibited not registered; P2 graph node capability unchecked; P3 reach only the root; P4 edge direction swapped; P5 second graph overwrites the first; P6 writes inverted; P7 unknown trust profile accepted; P8 data after the pack accepted; P9 repeated graph ID; P10 repeated intent node; P11 edge dependency unchecked; P12 edge dependent unchecked; P13 root unchecked; P14 no-root graph; P15 repeated graph node; P16 repeated trust profile; P17 set unsorted; P18 unprofiled node has a profile; P19 registry refusal ignored; C1 every capability registered; C2 prohibited role-allowed; C3 role ignored; C4 role case-folded; C5 unknown or prohibited root authorizes an empty set; C6 prohibited root authorized; C7 set not copied; C8 failed store swallowed; C9 wrong tenant asked; C10 version unchecked; C11 done context answered; C12 tenant unchecked; C13 prohibited flag dropped; S1 scope = known node; S2 scope ignores tenant; S3 done context answered; A1 empty role list allows all; A2 role case-folded; A3 empty role allowed; A4 unknown node allowed; E1 escalation includes every node; E2 missing graph gives the empty set and no error; E3 only the first write; E4 empty graph ID uses any graph; E5 nil pack answered. Five survived the tests of the GREEN commit (P12, P14, P15, C11, S3; re-checked against those tests) and each got a test (commit 3); three mutants (C1, C6, C13) did not compile in the harness and were redone.
- Regression cases added: the above; none is a bug fix.
- Debt entries: none. `helpers_test.go` and the pack refusal table are long by design (review trigger: `capabilities_test.go` is 406 lines, a table-driven suite of one behaviour per test; no other file is over 400).
- Conflicts raised: none. A319 (no `kind` field naming the escalation graph) is why the graph ID is a parameter.

## Built / not built
| Port or item | State | Data source |
|---|---|---|
| `Capabilities.Requested` (T4) | built | `execution/registry` over the pack's descriptors; the remedy from `Remedies` |
| `Capabilities.Check` (T14, A255) | built | registry + descriptor `authority.roles_allowed` |
| `Capabilities.Authorized` (A306) | built | the pack's action graphs (`action_graph_files`), strict contract decode |
| `Scope` (T5) | built, pack-wide (Decision 3) | intent nodes and their `trust_profile` |
| `Authority` (T6) | built | the trust profile's `authority.roles_allowed` |
| `EscalationSetFromPack` | built | WRITE nodes of the graph the caller names |
| `Remedies` (the stored intent's `requested_remedy` value) | **port only, not implemented** | the intent store: EL's pgx adapter (below) |
| Wiring in `cmd/core` | **not done** (wire_trust.go is EL's lane, A313) | see the README for the exact calls and what `main.go` passes |
| Mounting the packs directory into the core container | **not done**: `deploy/compose` mounts `packs` for the runner only | deploy lane |

## Decisions made (interim; each has a test; for the Architect)
1. **`Authorized`, unknown or prohibited capability: an error (`ErrCapability`), never none.** The intake ends UNKNOWN. T4 refuses such a capability first (REJECT), so this is the second wall (INV-16, BD-08).
2. **`Authorized`, a registered capability that roots no graph, or no remedy: the empty set, no error.** The intake adds the intent's own capability, so the set authorizes only that and widens nothing; T14 then refuses any step outside it (PLAN_INVALID). An error here would turn every ordinary read intent that names a capability but no graph into UNKNOWN. Alternative if the Architect prefers: an error for both; one line to change, two tests to flip.
3. **`Scope` is the pack's, the same for every tenant.** The pack holds no per-tenant list of nodes, and no tenant maps to another pack. An intent node is in scope when the pack has it and gives it a trust profile; the pack's node for a request outside its scope has none (PERD REQ-INT-005, PUS-03). A tenant-specific narrowing needs data that does not exist; open question below.
4. **`Authorized` follows the edges from the root** (nodes that depend on it) and not the whole graph, so a graph with two declared roots gives each its own downstream; for the fintech pack each graph has one root and the answer is the whole graph. Over several graphs rooted at one capability it is the union.
5. **A prohibited capability is `Registered` true and `Prohibited` true** (REQ-CAP-002: registered, rejecting). The intake refuses `!Registered || Prohibited`, so either reading refuses.
6. **`LoadPack` refuses the whole pack** when a graph node names a capability that is not registered or is prohibited, and on the structural faults listed in the README. `packcheck` (Python) checks the same and more, but the core must not rely on a CI step to be safe at runtime.
7. **The remedy comes through a consumer-defined `Remedies` port**, because `intake.AdmittedIntent` carries no slot value (it has the node, the target, the hash and the view). The alternative is a new field `AdmittedIntent.RequestedCapability` filled by the intent store's adapter (as `TargetRef` is): it would let `Verify` cover it and save a second read, but it changes a Tier 1 type in `trust/intake`, which this order did not authorize. Not done; raised.

## Questions for the Architect and EL (the data is not guessed)
- Q1 Decisions 1, 2 and 3 above.
- Q2 `AdmittedIntent.RequestedCapability` instead of `Remedies` (Decision 7)? If yes: one field, the intent adapter sets it, `Requested` and `Authorized` read it, the port goes.
- Q3 EL: the `Remedies` implementation over the intent store. The stored value is `requested_remedy` (a Reference slot: the test stand-in reads it as a string); it returns present=false only for an absent slot, and an unresolved or malformed slot should be an error, not "absent".
- Q4 Tenant-specific scope: where would a tenant's pack or node list live (the A313 lane)?
- Q5 `cmd/core` must be able to read the packs directory (config and mount); and the escalation graph ID is configuration (A319, DEBT-48 until the pack carries a `kind`).
- Q6 The pack files the order names (`packs/fintech/graph/*.json`) are loaded through `action_graph_files` of `packs/fintech_v1.json`, relative to the packs directory. `packs/fintech/trust/` and `packs/fintech/capabilities/` hold only READMEs ("empty until PCK-01 replaces `fintech_v1.json`"), so the trust profiles and descriptors are read from the pack file. If PCK-01 splits the pack into those folders, only `LoadPack` changes.

## Commands and results (all run in `/home/user/wt-ad2/services/core` unless noted)
- `go test -race -count=1 ./orchestrator/portadapters/trustpack`: ok (40 tests).
- `go test -count=1 ./execution/registry ./orchestrator/... ./trust/...`: ok (the registry's scan test, which reads every non-test Go file for a prohibited operation's name, passes with the new package).
- `gofmt -l` and `go vet ./orchestrator/portadapters/trustpack`: clean.
- `python3 tools/lint/run.py` (repository root): lint OK. No `testdata/rapid/*.fail` was produced (no rapid test in the package).

## Pre-work for the next agent
- Exact next step: the Architect answers Q1 to Q4; then push, open the PR (task TRU-02 follow-up; REQ-TRU-004, REQ-TRU-005, REQ-TRU-010; Tier 1: Codex, SR and one independent role review); the PR body needs the plan, the SoT 52 questions and the checklist. EL wires the four constructors in `wire_trust.go` and writes `Remedies` over the intent store.
- Open questions (owner): Q1 to Q6 above (Architect, EL).
- Files to read first: `trustpack/README.md`, `pack.go` (`LoadPack`, `loadGraph`), `capabilities.go` (`Authorized`).
- Do not touch: `trust/intake` and `trust/recorder` (the ports and the set type are used as they are); the pack files.
