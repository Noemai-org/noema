import { createElement as h } from "react";
import { ContractError, parseReconstructionV3 } from "./contract_types/operator.js";

// Presentation only. Authorization and the access Record are enforced by the core.
export function EvidencePanel({ reconstruction }: { reconstruction: unknown }) {
  try {
    parseReconstructionV3(reconstruction);
  } catch (error) {
    if (!(error instanceof ContractError)) throw error;
    return h("p", { role: "status" }, "This evidence is unavailable.");
  }
  return h("section", { "aria-label": "Record" }, h("h2", null, "Record"), h("span", null, "Verified"));
}
