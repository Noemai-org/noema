# Status · TRU-07 follow-up: the Confirmations port carries the fresh binding (PR 1 of the confirmation lanes)

Updated 2026-11-19. Owner: C1 (agent session). Branch `prep/confirm-port`. Not pushed; no PR yet.

# Session 2026-11-19 · TRU-07 confirm port

## Session summary
- What changed (PR links): none yet (local commits only). `trust/intake`: `ConfirmationDeclined` (value 4, after Given; the comment of `ConfirmationPending` no longer folds a decline into Pending); `FreshBinding` (`EvidenceSetHash`, `PolicyVersion`); `RequestSpec` (`Requirement`, `TTL`); `Confirmation` gains `RequestID` and `NewRequest *RequestSpec`; the port is now `Confirmations.Status(ctx, AdmittedIntent, FreshBinding)`; `SnapshotEvidence.EvidenceSetHash`. T9 refuses a non-empty value outside the hash form (`ErrBadEvidence`, UNKNOWN) and keeps the value in the evaluation; T13 builds `FreshBinding` and passes it. README line for T13 updated.
- Why: the Confirmations adapter (EL) must recompute the binding of a Given answer (`confirm.BindingHash`, SoT 43.2a) from the stored request plus the two values only the fresh evaluation knows, and must tell Trust whether a request is open or must be created. This PR changes the port only; it adds no behaviour.
- Behaviour in this version: NotRequired, Pending (CONFIRM) and Given are exactly as before. `ConfirmationDeclined`, like the zero value and any unknown status, ends in UNKNOWN (`errAnswer`, INTERNAL_ERROR); nothing is planned or recorded. `RequestID` and `NewRequest` are carried and not read. Empty `FreshBinding` values are passed on as empty and fail nothing in Trust.
- Risks, and how they are contained: (a) the policy version rule is computed locally (`freshBinding` in `gates_t13_t15.go`), because the intake package doc lists no `trust/grants` import; it is "id@version" only when exactly one distinct (id, version) took part in T11, else empty (see open questions). (b) A test that used `ConfirmationGiven + 1` as "a status outside the three" would have silently become Declined; it now uses `ConfirmationDeclined + 1`. (c) Recorder, recorder package, contracts and all other packages are untouched.
- Tests added (REQ-TRU-009, INV-25, INV-16; `t13_binding_test.go`): T13 passes the T9 hash and the T11 "id@version"; one version listed twice is one; two policies, two versions of one policy and no policy give an empty policy version; empty values pass through and fail nothing; T9 keeps the hash; a malformed non-empty hash (five forms) is UNKNOWN at T9 and Confirmations.Status is never called; Declined is its own status (1, 2, 3, 4); Declined (bare, and carrying an event and plan hash) is UNKNOWN with nothing planned or recorded (interim); Pending with a `RequestID` and with a `NewRequest` still yields CONFIRM.
- Regression cases added: none (no defect fixed).
- Mutants run by hand on the source, each killed, source restored after each: swap the two `FreshBinding` fields; drop the evidence hash; drop the policy version; version and id swapped in the string; T9 does not keep the hash; T9 accepts a malformed hash; T9 refuses an empty hash; Declined handled like Pending; Declined handled like Given (killed only after the "names an event" case was added to the Declined test); policy chosen when several differ. Two mutants are equivalent (taking the last instead of the first policy version as the reference; `< 1` instead of `< 0` on the index of a different element) because the check only asks whether any element differs.
- Debt entries: none. Conflicts raised: none.

## What is for EL: the Confirmations adapter
Signature (in `trust/intake`, `ports_t13_t15.go`):

```go
Status(ctx context.Context, a AdmittedIntent, f FreshBinding) (Confirmation, error)
```

- `f` is the fresh evaluation's `EvidenceSetHash` (the sealed snapshot's `evidence_set_hash`, hash form `^sha256:[0-9a-f]{64}$`, or empty) and `PolicyVersion` ("policy_id@version" of the single policy version that took part in T11, or empty). Recompute the binding of a Given answer with `confirm.BindingHash` from the stored request's other six values (principal, intent hash, capability, target, params hash, plan hash) and these two. Return an error (never a Given answer) when either value is empty. For a read where no write plan exists, the six values of the stored request are whatever the request holds.
- The Evidence adapter must fill `SnapshotEvidence.EvidenceSetHash` from the sealed snapshot; if it leaves it empty, no confirmation can ever resolve to Given.
- Statuses: `ConfirmationNotRequired` (the profile asks for none); `ConfirmationPending` (the profile asks for one and there is no valid resolved confirmation: none yet, expired, not eligible, or the binding changed); `ConfirmationGiven` (a valid ACCEPT resolved to this evaluation: set `EventID` and `PlanHash`, as before, plus `RequestID`); `ConfirmationDeclined` (the approver declined: set `RequestID`). A decline must not be reported as Pending.
- `RequestID`: for Pending, the open, unexpired request if one exists; for Given and Declined, the request that was answered.
- `NewRequest` (Pending only): non-nil means no open request exists and Trust must create one from the spec; nil with a `RequestID` means one is already open and no second request is made. `RequestSpec.Requirement` is SELF, MAKER_CHECKER or ROLE:<role> (the contract's ApproverRequirement); `TTL` comes from the pack or the configuration and Trust never picks a number (AGENT-16). The requester is not in the spec: Trust takes it from the admitted intent's `PrincipalID` (INV-12).
- In this version Trust reads `Status`, `EventID` and `PlanHash` only; `RequestID`, `NewRequest` and Declined are acted on in the next PR (Declined becomes REJECT `DECLINED_BY_APPROVER` there). Until then a Declined answer is UNKNOWN.

## Open questions
- Policy version when one policy took part in more than one version (owner: Architect or EL): `trust/grants.BoundPolicy` picks the newest version of the one named policy by a dotted comparison; the intake rule here returns empty unless exactly one distinct (id, version) took part. Empty makes the adapter refuse a Given answer, so the evaluation fails closed. If both must agree in that case, either the intake may import `trust/grants.BoundPolicy`'s rule (the intake doc's import list would change) or the rule moves to a package both may import. Not decided here.
- Whether T13 should itself refuse an empty `EvidenceSetHash` or `PolicyVersion` for a required confirmation: the task says no in this PR; the adapter refusal is the only check.
- `Confirmation.PlanHash` for a Declined answer: not read; the adapter may leave it empty.

## Pre-work for the next agent
- Exact next step: PR 2 turns `ConfirmationDeclined` into REJECT `DECLINED_BY_APPROVER` and uses `RequestID` and `NewRequest` (create the request with the intent's `PrincipalID`, no second request when one is open); the test `TestT13DeclinedIsUnknownForNowAndNothingIsPlannedOrRecorded` changes with it.
- Files to read first: `services/core/trust/intake/ports_t13_t15.go`, `gates_t13_t15.go` (`t13`, `freshBinding`), `gates_t7_t9.go` (`t9`), `t13_binding_test.go`, `services/core/trust/confirm/confirm.go` (`BindingInput`).
- Do not touch: `trust/recorder`, `internal/contracts`, `/contracts`.
