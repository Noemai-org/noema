# Status · EVD-02 (Core Engineer 2) · effective freshness

Updated 2026-11-24. Plan task EVD-02 (owner C2; REQ-CTX-003). Needs EVD-01: Done (tasks.csv; noema#533).

# Session 2026-11-24 · EVD-02 slice 1

## Session summary
- What changed: `evidence/freshness/{freshness.go,freshness_test.go}`; `evidence/layer/status.go` (its private `effectiveLimit` delegates to `freshness.Effective`, no behaviour change: the layer tests that need no database pass unchanged); `evidence/README.md`.
- Why: REQ-CTX-003, INV-23, INV-16, INV-21; the Founder's order of 24 Nov 2026 (relayed, interim).
- Risks: none for behaviour (a delegation of one pure function). The package is new and nothing calls `Session` yet.
- Tests added (REQ-CTX-003, INV-23): the strictest limit and `limit_set_by` (ties go to the earlier of source, profile, override); no usable limit refused, never defaulted; boundary at limit minus one millisecond, exactly the limit, plus one millisecond and one nanosecond; a property test with a generated clock skew over 50,000 seeded cases; a fresh item is not re-read; a stale one is re-read once and `EVIDENCE_REFRESHED` is recorded with both ids and the limit that applied; a failed, still-stale, timed-out or unrecordable refresh is UNAVAILABLE with no item; a second stale result for a source is not refreshed again; 50 concurrent asks refresh once (`-race`); constructor refusals.
- Hand mutants (9, all killed): override branch removed; boundary `>=`; tie to the later limit; sink error ignored; refresh not counted before it runs; no deadline (the test hangs and the run is killed); stale item returned on failure; a stale refresh accepted; a read from the future treated as fresh.
- Regression cases: none (new code). Debt entries: none. Conflicts raised: none.

## OPEN entries for the Steward (nothing is invented; nothing is set)
- **OD-EVD02-1 (owner: Founders).** PERD 21.2 names `per_source_override?` in the effective limit and `PROFILE_OVERRIDE` in `limit_set_by`, but neither the trust profile (`max_evidence_age_seconds` only) nor any pack member or contract carries an override. `freshness.Limits.Override` is an input that nothing supplies, so it is zero (none). Safest default: no override. Decision needed: where a per-source override lives (a pack member on the trust profile, additive, AGENT-15) and who sets its values (AGENT-16).
- **OD-EVD02-2 (owner: Architect).** The read budget of REQ-CTX-003 ("within the read budget") has no number in the documents I read for this slice; `Session` takes the refresh timeout as a required argument and sets no default. Safest default: the caller must pass one.

## Pre-work for the next agent
- Exact next step (slice 2): (a) the `Sink` backed by `chain.Appender` that appends `EVIDENCE_REFRESHED` as `core-evidence` in the same transaction as the refreshed item's `EVIDENCE_READ` (a DB test, CI only); (b) Trust's evidence read path calls `Session.Ensure` per source (EL's composition; `orchestrator/portadapters/trust/evidence.go` still computes the limit inline at one place and should call `freshness.Effective`: it is outside this card's allowlist, so it is a follow-up for its owner); (c) revalidation at dispatch (the Execution side reads `revalidation_reads` through the same package; REQ-EXE-003).
- Open questions (owner): OD-EVD02-1 and OD-EVD02-2 above.
- Files to read first: `evidence/freshness/freshness.go`, PERD 21.1 and 21.2, REQ-CTX-003.
- Do not touch: freshness limits in the pack (AGENT-16).
