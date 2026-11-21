import { createElement as h } from "react";
import { ContractError, parseScorecardViewV1 } from "./contract_types/benchmark.js";

// Synthetic presentation slice only. Do not mount until the generated parser's
// conditional-validation changes and the bench-only static source are ready.
export function ScorecardInspector({ scorecard: input }: { scorecard: unknown }) {
  let scorecard;
  try { scorecard = parseScorecardViewV1(input); }
  catch (error) {
    if (!(error instanceof ContractError)) throw error;
    return h("p", { role: "status" }, "This scorecard is unavailable.");
  }
  // Reject unsafe displayed counts: JSON.parse may already have rounded them.
  // This cannot recover source numeric text; the bench input remains unmounted
  // until the complete lossless boundary in #808 is agreed and tested.
  if (scorecard.tier0.gates.some((gate) => !Number.isSafeInteger(gate.count)) ||
      Object.values(scorecard.by_family).some((result) =>
        !Number.isSafeInteger(result.n) || !Number.isSafeInteger(result.gate_trips))) {
    return h("p", { role: "status" }, "This scorecard is unavailable.");
  }
  return h("section", { "aria-label": "Benchmark scorecard" },
    h("h2", null, "Tier 0"),
    h("ul", null, ...scorecard.tier0.gates.map((gate) => h("li", { key: gate.gate },
      gate.gate.toLowerCase().replaceAll("_", " "), ": ", String(gate.count),
      gate.rate.exact_upper95 === null ? null : `; exact upper bound: ${String(gate.rate.exact_upper95)}`))),
    h("h2", null, "Tier 1"),
    h("p", null, "Correct terminal handling: ", scorecard.tier1.correct_terminal_handling.rate === null
      ? "Not available" : String(scorecard.tier1.correct_terminal_handling.rate)),
    h("h2", null, "By family"),
    h("ul", null, ...Object.entries(scorecard.by_family).map(([family, result]) =>
      h("li", { key: family }, family, ": ", String(result.n), " cases; ", String(result.gate_trips), " gate trips"))),
  );
}
