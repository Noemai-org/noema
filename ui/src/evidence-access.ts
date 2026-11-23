import { createElement as h } from "react";
import { EvidencePanel } from "./evidence-panel.js";

// The server decides access. Do not distinguish a denied resource from a missing one.
export function EvidenceResult({ status, reconstruction }: { status: number; reconstruction?: unknown }) {
  if (status !== 200) return h("p", { role: "status" }, "This is unavailable.");
  return h(EvidencePanel, { reconstruction });
}
