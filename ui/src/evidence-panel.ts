import { createElement as h } from "react";
import { ContractError, parseReconstructionV3, type ReconstructionV3SectionKind } from "./contract_types/operator.js";

const headings: Record<ReconstructionV3SectionKind, string> = {
  WHO: "Who", REQUEST: "Request", AMBIGUITY: "Clarification", RESOLUTION: "Resolution",
  OBSERVED_STATE: "Observed state", POLICY: "Policy", AUTHORITY: "Authority", DECISION: "Decision",
  ACTION: "Action", DOWNSTREAM_RESULT: "Downstream result", VERIFICATION: "Verification", EXPLANATION: "Explanation",
};

const decision: Record<string, string> = { ALLOW: "Allowed", CLARIFY: "Clarification needed", CONFIRM: "Confirmation needed", WAIT: "Waiting", ESCALATE: "Escalated", REJECT: "Rejected", UNKNOWN: "Unknown" };
const execution: Record<string, string> = { DISPATCHED: "Dispatched", ACKNOWLEDGED: "Acknowledged", OUTCOME_UNKNOWN: "Outcome unknown", REJECTED_BY_TARGET: "Rejected by target", REJECTED_STALE_VERSION: "Rejected due to stale version", FAILED_TERMINAL: "Failed" };
const grant: Record<string, string> = { VALID: "Valid", MISSING: "Missing", EXPIRED: "Expired", SPENT: "Spent", UNMATCHED: "Unmatched" };
const verdict: Record<string, string> = { ACHIEVED: "Achieved", NOT_ACHIEVED: "Not achieved", PARTIALLY_ACHIEVED: "Partially achieved", PENDING_EXTERNAL: "Waiting for an external result", UNKNOWN: "Unknown", COMPENSATED: "Compensated" };

// Values are mapped from the core's enums; unknown or absent values are not rendered.
function mapped(facts: Record<string, unknown>, field: string, labels: Record<string, string>, title: string) {
  const input = facts[field];
  return typeof input === "string" && Object.hasOwn(labels, input) ? `${title}: ${labels[input]}` : null;
}

function details(kind: ReconstructionV3SectionKind, facts: Record<string, unknown>): string[] {
  const out: (string | null)[] = [];
  if (kind === "RESOLUTION" && Number.isInteger(facts.candidate_count) && typeof facts.candidate_count === "number" && facts.candidate_count >= 0 && facts.candidate_count <= 20) out.push(`Candidates: ${facts.candidate_count}`);
  if (kind === "DECISION") out.push(mapped(facts, "decision_outcome", decision, "Decision"));
  if (kind === "ACTION") out.push(mapped(facts, "execution_state", execution, "Execution"), mapped(facts, "grant_state", grant, "Grant"));
  if (kind === "VERIFICATION") out.push(mapped(facts, "verdict", verdict, "Verdict"));
  if (kind === "EXPLANATION" && typeof facts.text === "string") out.push(facts.text);
  return out.filter((text): text is string => text !== null);
}

// Authorization and the access Record must still be enforced by the core.
// This component remains unmounted until its ops/audit boundary is integrated.
export function EvidencePanel({ reconstruction }: { reconstruction: unknown }) {
  let value;
  try {
    value = parseReconstructionV3(reconstruction);
  } catch (error) {
    if (!(error instanceof ContractError)) throw error;
    return h("p", { role: "status" }, "This evidence is unavailable.");
  }
  const chainVerdict = value.timeline.find((entry) => entry.event_type === "VERDICT");
  return h("section", { "aria-label": "Record" }, h("h2", null, "Record"),
    chainVerdict ? h("p", { role: "status" }, chainVerdict.text) : null,
    ...value.sections.map((section, index) => h("section", { key: index },
      h("h3", null, headings[section.kind]),
      ...section.items.flatMap((item, i) => details(section.kind, item.facts).map((text, j) => h("p", { key: `${i}-${j}` }, text))),
    )));
}
