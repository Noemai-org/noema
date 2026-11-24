import { fetchInvestigation } from "./investigation-client.js";
import type { InvestigationResponseV1 } from "./contract_types/customer.js";

// A654: the UI polls at two-second intervals; the host supplies the 60-second ceiling.
const POLL_INTERVAL_MS = 2_000;

interface PollInput {
  baseUrl: string;
  investigationId: string;
  sessionToken: string;
  maxResponseBytes: number;
  maxPollingMs: number;
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

/** Poll only core-supplied state, for no longer than the host-approved duration. */
export async function pollInvestigation(input: PollInput): Promise<"Connection lost." | undefined> {
  if (!Number.isSafeInteger(input.maxPollingMs) || input.maxPollingMs <= 0 || input.maxPollingMs > 2_147_483_647) return "Connection lost.";
  if (input.signal?.aborted) throw new DOMException("Aborted", "AbortError");
  const controller = new AbortController();
  let expired = false;
  const abort = () => controller.abort();
  input.signal?.addEventListener("abort", abort, { once: true });
  const timer = setTimeout(() => { expired = true; controller.abort(); }, input.maxPollingMs);
  try {
    if (input.signal?.aborted) controller.abort();
    return await pollUntilDone({ ...input, signal: controller.signal });
  } catch (error) {
    if (input.signal?.aborted) throw new DOMException("Aborted", "AbortError");
    if (expired) return "Connection lost.";
    throw error;
  } finally {
    clearTimeout(timer);
    input.signal?.removeEventListener("abort", abort);
  }
}

async function pollUntilDone(input: PollInput): Promise<"Connection lost." | undefined> {
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
    await wait(POLL_INTERVAL_MS, input.signal);
  }
}
