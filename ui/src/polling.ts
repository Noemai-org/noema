import { fetchInvestigation } from "./investigation-client.js";
import type { InvestigationResponseV1 } from "./contract_types/customer.js";

interface PollInput {
  baseUrl: string;
  investigationId: string;
  sessionToken: string;
  maxResponseBytes: number;
  signal?: AbortSignal;
  onState: (state: InvestigationResponseV1) => void;
}

function wait(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) { reject(new DOMException("Aborted", "AbortError")); return; }
    const abort = () => { clearTimeout(timer); reject(new DOMException("Aborted", "AbortError")); };
    const timer = setTimeout(() => { signal?.removeEventListener("abort", abort); resolve(); }, ms);
    signal?.addEventListener("abort", abort, { once: true });
  });
}

/** Poll only core-supplied state. Limits are supplied by the host until the wire bound is specified. */
export async function pollInvestigation(input: PollInput): Promise<"Connection lost." | undefined> {
  for (;;) {
    let state: InvestigationResponseV1;
    try {
      state = await fetchInvestigation(input);
    } catch {
      if (input.signal?.aborted) throw new DOMException("Aborted", "AbortError");
      let recovered: InvestigationResponseV1 | undefined;
      for (const delay of [0, 1000, 2000, 4000]) {
        if (delay) await wait(delay, input.signal);
        try { recovered = await fetchInvestigation(input); break; }
        catch { if (input.signal?.aborted) throw new DOMException("Aborted", "AbortError"); }
      }
      if (!recovered) return "Connection lost.";
      state = recovered;
    }
    input.onState(state);
    if (state.state !== "understanding" && state.state !== "working") return;
    await wait(1000, input.signal);
  }
}
