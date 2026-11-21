import { parseConversationResponseV1, parseMessageRequestV1, type InvestigationResponseV1 } from "./contract_types/customer.js";
import { boundedText, isSecureInvestigationTarget } from "./investigation-client.js";
import { pollInvestigation } from "./polling.js";
import { RESPONSE_FRAME_LIMIT_BYTES } from "./transport-limits.js";

type Input = {
  baseUrl: string;
  sessionToken: string;
  request: unknown;
  maxPollingMs: number;
  signal?: AbortSignal;
  onState: (state: InvestigationResponseV1) => void;
};

// A479: a valid shape alone does not establish the cross-member relations.
// Never poll or stream a different investigation from the one the core returned.
export async function startConversationAndPoll(input: Input): Promise<"Connection lost." | undefined> {
  try {
    const url = new URL("/v1/conversations", input.baseUrl);
    const body = parseMessageRequestV1(input.request);
    if (!isSecureInvestigationTarget(url) || !input.sessionToken || typeof body.text !== "string" ||
        !Number.isSafeInteger(input.maxPollingMs) || input.maxPollingMs <= 0 || input.maxPollingMs > 2_147_483_647) return "Connection lost.";
    const response = await fetch(url, {
      method: "POST", headers: { "content-type": "application/json", authorization: `Bearer ${input.sessionToken}` },
      body: JSON.stringify(body), credentials: "omit", cache: "no-store", redirect: "error", signal: input.signal,
    });
    const contentType = response.headers.get("content-type")?.split(";")[0]?.trim().toLowerCase();
    if ((response.status !== 200 && response.status !== 202) || contentType !== "application/json") {
      await response.body?.cancel();
      return "Connection lost.";
    }
    const answer = parseConversationResponseV1(JSON.parse(await boundedText(response, RESPONSE_FRAME_LIMIT_BYTES)));
    if (answer.conversation_ref !== answer.investigation_id ||
        answer.stream_url !== `/v1/conversations/${answer.investigation_id}/events` ||
        answer.correlation_id !== `corr_${answer.investigation_id.slice(4)}`) return "Connection lost.";
    return await pollInvestigation({ ...input, investigationId: answer.investigation_id, maxResponseBytes: RESPONSE_FRAME_LIMIT_BYTES });
  } catch {
    if (input.signal?.aborted) throw new DOMException("Aborted", "AbortError");
    return "Connection lost.";
  }
}
