# Status · G1 stand-in data (Benchmark Owner agent)

Updated 2026-11-04. Decision: the Founder, 4 Nov 2026 (Option B). Related: BEN-05, `benchmark/fixtures/`.

## State
- **Decided:** G1 runs on a Noema-side stand-in for the US-01 and US-06 data; evidence labelled "stand-in"; the same scenarios rerun against live Tushe once its OpenAPI is frozen. `monetary_calls` source: (a) a per-route request log in the hostility proxy, built by the Integration Lead. No `tushe-admin` job is added. Secret names given by the Founder: `TUSHE_RUNTIME_KEY`, `TUSHE_FIXTURE_KEY`, `TUSHE_ADMIN_TOKEN` (recorded here; no workflow reads them, so none is added to `.env.example` by this change).
- **Built (this change):** `benchmark/fixtures/standin.py` (the data contract), `loader.py` (the benchmark's half of the loader), the runner flag `--standin-url`. Tests first (seen failing).
- **Not built (not mine):** the stand-in service itself and the proxy's per-route request log (Integration Lead); the core's reading of the stand-in (adapters, paused until the Founder unpauses for the stand-in); the session provider (EDG-01).

## Open
1. The stand-in service and its read routes need an owner and a date (Integration Lead). Proposal in `benchmark/fixtures/README.md`.
2. The proxy log format for the monetary counter: proposed on noema#117.
3. Existing cases in a fixture need a mock-case seed route; none of the G1 cases has any.

## Messages
### 2026-11-04 (time not recorded) accepted · Founder (decisions, relayed by the user) → Benchmark Owner agent
- Purpose: option B for G1; the monetary source; no `tushe-admin` job; the three secret names.
- Receiver used it for: this change and the comment to the Integration Lead.
- Evidence: this pull request.
