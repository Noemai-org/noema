import { createElement as h } from "react";
import { ContractError, parseReconstructionV3, type ReconstructionV3SectionKind } from "./contract_types/operator.js";

const headings: Record<ReconstructionV3SectionKind, string> = {
  WHO: "Who", REQUEST: "Request", AMBIGUITY: "Clarification", RESOLUTION: "Resolution",
  OBSERVED_STATE: "Observed state", POLICY: "Policy", AUTHORITY: "Authority", DECISION: "Decision",
  ACTION: "Action", DOWNSTREAM_RESULT: "Downstream result", VERIFICATION: "Verification", EXPLANATION: "Explanation",
};

// Presentation only. Authorization and the access Record are enforced by the core.
export function EvidencePanel({ reconstruction }: { reconstruction: unknown }) {
  let value;
  try {
    value = parseReconstructionV3(reconstruction);
  } catch (error) {
    if (!(error instanceof ContractError)) throw error;
    return h("p", { role: "status" }, "This evidence is unavailable.");
  }
  return h("section", { "aria-label": "Record" }, h("h2", null, "Record"), h("span", null, "Verified"),
    ...value.sections.map((section, index) => h("section", { key: index },
      h("h3", null, headings[section.kind]),
      ...(section.kind === "EXPLANATION" ? section.items.map((item, i) =>
        typeof item.facts.text === "string" ? h("p", { key: i }, item.facts.text) : null) : []),
    )));
}
