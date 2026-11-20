import { createElement as h } from "react";
import { ContractError, parseScorecardViewV1 } from "./contract_types/benchmark.js";

// Synthetic presentation slice. Do not mount until the conditional parser and
// bench-only static input/access boundary are integrated.
export function BenchmarkLatency({ scorecard: input }: { scorecard: unknown }) {
  let scorecard;
  try { scorecard = parseScorecardViewV1(input); }
  catch (error) {
    if (!(error instanceof ContractError)) throw error;
    return h("p", { role: "status" }, "This scorecard is unavailable.");
  }
  const value = (n: number | null) => n === null ? "Not available" : String(n);
  return h("section", { "aria-label": "Latency by path" },
    h("h2", null, "Latency by path"),
    h("ul", null, ...Object.entries(scorecard.tier3.latency_by_path).map(([path, latency]) =>
      h("li", { key: path }, path.toLowerCase().replaceAll("_", " "), ": ",
        "Count: ", value(latency.n), "; median: ", value(latency.p50_ms), " ms; ",
        "95th percentile: ", value(latency.p95_ms), " ms; ",
        "99th percentile: ", value(latency.p99_ms), " ms"))));
}
