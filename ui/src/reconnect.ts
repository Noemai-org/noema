import { parseCommonV1InvestigationId } from "./contract_types/common_v1.js";
import type { InvestigationResponseV1, SseEventV1 } from "./contract_types/customer.js";
import { fetchInvestigation, isSecureInvestigationTarget } from "./investigation-client.js";
import { pollInvestigation } from "./polling.js";
import { POLL_MAX_DURATION_MS } from "./transport-limits.js";
import { readEventStream, StreamFrameTooLargeError, StreamInterruptedError, StreamProtocolError } from "./sse-reader.js";

interface ReconnectInput {
  baseUrl: string;
  conversationRef: string;
  investigationId: string;
  sessionToken: string;
  maxFrameBytes: number;
  maxResponseBytes: number;
  signal?: AbortSignal;
  onEvent: (event: SseEventV1) => void;
  onState: (state: InvestigationResponseV1) => void;
}

async function stream(input: ReconnectInput, onEvent: (event: SseEventV1) => void): Promise<void> {
  const id = parseCommonV1InvestigationId(input.conversationRef);
  const url = new URL(`/v1/conversations/${encodeURIComponent(id)}/events`, input.baseUrl);
  if (!isSecureInvestigationTarget(url) || !input.sessionToken) throw new StreamProtocolError();
  const response = await fetch(url, {
    method: "GET", headers: { Authorization: `Bearer ${input.sessionToken}` },
    credentials: "omit", cache: "no-store", redirect: "error", signal: input.signal,
  });
  await readEventStream(response, onEvent, input.maxFrameBytes);
}

async function pollFallback(input: ReconnectInput): Promise<void> {
  const result = await pollInvestigation({
    ...input,
    maxPollingMs: POLL_MAX_DURATION_MS,
    onState: input.onState,
  });
  if (result) throw new StreamInterruptedError();
}

/** Oversized SSE frames and failed reconnects fall back to authoritative 2-second polling. */
export async function reconnectOnce(input: ReconnectInput): Promise<void> {
  let consumerFailed = false;
  let answerReceived = false;
  const onEvent = (event: SseEventV1) => {
    try { input.onEvent(event); if (event.event === "answer") answerReceived = true; }
    catch (error) { consumerFailed = true; throw error; }
  };
  try {
    await stream(input, onEvent);
  } catch (error) {
    if (consumerFailed || input.signal?.aborted) throw error;
    if (error instanceof StreamFrameTooLargeError) {
      await pollFallback(input);
      return;
    }
    if (error instanceof StreamProtocolError || !(error instanceof TypeError || error instanceof StreamInterruptedError)) throw error;
  }
  if (answerReceived) return;
  try {
    const state = await fetchInvestigation(input);
    input.onState(state);
    if (state.state === "answered" && state.last_answer !== undefined) return;
    await stream(input, onEvent);
  } catch (error) {
    if (consumerFailed || input.signal?.aborted) throw error;
    if (error instanceof StreamProtocolError && !(error instanceof StreamFrameTooLargeError)) throw error;
    if (error instanceof TypeError || error instanceof StreamInterruptedError || error instanceof StreamFrameTooLargeError) {
      await pollFallback(input);
      return;
    }
    throw error;
  }
}
