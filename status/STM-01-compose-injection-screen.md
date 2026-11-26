# Status · STM-01 · compose injection screen inputs (A704)

State: red and green committed locally; not pushed.

# Session 2026-11-26 · STM-01

## Session summary
- What changed (PR links): none yet. `cmd/core/compose_injection.go` (new): the five required `NOEMA_INJECTION_*` settings, `newInjectionScreen`, `buildInjectionScreen`, the Alert, `correlationOfContext`, the failure counter. `compose.go`: one call, a `Components.InjectionFailures` field, the screen handed to `trustWiringIn`. `wire_readiness_test.go`: the new field is listed as not a component. `deploy/compose/bench-stack.yml`: the five ruling values.
- Why: A704 (the inputs of the injection screen), A330 and AGENT-16 (no default in code).
- Risks, and how they are contained: no Record port is wired, so in production every finding is a Record failure that raises the Alert and the counter (never a silent pass); a payload is still admitted (advisory, SoT 29). An invalid setting stops the process; an absent one is a named gap.
- Tests added (REQ-STM-001, REQ-CTX-007, REQ-AUD-001; INV-16, INV-19; AGENT-13, AGENT-16): `compose_injection_test.go`. 19 hand mutants, all killed.
- Regression cases added: the same tests.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: wire the `injection.Recorder` (`RecordRecorder`, `core-evidence` through the tenant's evidence login, A536) and pass it at the one call in `compose.go`; then the Evidence, Policies, Reconciler and Recorder lanes, in order.
- Open questions (owner): where `noema_injection_record_failures_total` is exported (EL: no metric registry exists on main).
- Files to read first: `cmd/core/compose_injection.go`.
- Do not touch: `evidence/injection`, `evidence/payloadread`, `platform/telemetry`.

# Session 2026-11-26 · STM-01 (A722 follow-up)

## Session summary
- What changed (PR links): none yet; follow-up to noema#1018. `compose_injection.go`: the screen no longer loads the pack; `buildInjectionScreen(env, verified, log, rec)` takes the composition's `VerifiedSource`; the three required `NOEMA_EVIDENCE_*` settings (`evidenceSettingsFrom`): absent is a named gap (after the limits), an invalid overlap is fatal, no code default. `compose_packsource.go`: `VerifiedSource`, `loadVerified`, `packFromVerified`, `memoVerified`; `productionPack` is now built from them. `compose.go`: `composeDeps.Verified`; `productionDeps` shares one memoised load between the Trust pack source and the screen. `bench-stack.yml`: `NOEMA_EVIDENCE_DSN_DIR` and `NOEMA_EVIDENCE_KEY_MAX_ACTIVE: "2"`.
- Why: A722 (evidence login names; the composition carries the one verified pack, no fourth load), A594, A330, AGENT-16.
- Risks, and how they are contained: with the three settings absent the screen is a named gap on Evidence, Policies and Clocks (it is the evidence login that will record a finding, A664, A704). `NOEMA_EVIDENCE_KEY_FILE` has no bench value (no ruling names the core-evidence key path; IL provisions it, A536), so the bench screen stays a named gap until it is set. `checkPackLimits` (A612) and `composeUnderstanding` still load the pack themselves: not changed here, outside this seam.
- Tests added (REQ-STM-001, REQ-CTX-007; INV-16; AGENT-16): `compose_evidence_test.go`.
- Regression cases added: a source guard that `compose_injection.go` never loads a pack; the single-load test of `productionDeps`.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: IL names and provisions the core-evidence key file path and the evidence DSN volume (the DSN dir value `/run/evidence-dsn` follows the existing pattern; no volume mounts it yet); then set `NOEMA_EVIDENCE_KEY_FILE` in the bench Compose; EL wires the `injection.Recorder` that reads the three settings.
- Open questions (owner): the key file path (IL); whether `checkPackLimits` and `composeUnderstanding` should also take `VerifiedSource` (EL, the lanes that own them).
- Files to read first: `cmd/core/compose_injection.go`, `cmd/core/compose_packsource.go`.
- Do not touch: `evidence/injection`, `evidence/payloadread`, `platform/telemetry`.
