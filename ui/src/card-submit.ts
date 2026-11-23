import { ContractError } from "./contract_types/_runtime.js";
import { parseCommonV1InvestigationId } from "./contract_types/common_v1.js";
import { parseOverlayCardV1ClarificationCard, parseOverlayCardV1ConfirmationCard } from "./contract_types/overlay_card_v1.js";
import { parseErrorsV3, parseMessageRequestV1 } from "./contract_types/customer.js";
import { cardExpiry } from "./card-expiry.js";
import { parseConfirmationAnswerForPath } from "./confirmation.js";

type SubmitResult = "accepted" | "expired" | "unavailable";
type Shared = { baseUrl: string; sessionToken: string; signal?: AbortSignal };
type Clarification = Shared & { conversationRef: string; card: unknown; optionId: string; requestId: string };
type Confirmation = Shared & { card: unknown; response: "ACCEPT" | "DECLINE" };

export function isSecureTarget(url: URL): boolean {
  if (url.username || url.password) return false;
  if (url.protocol === "https:") return true;
  return url.protocol === "http:" && (url.hostname === "localhost" || url.hostname === "[::1]" || /^127(?:\.\d{1,3}){3}$/u.test(url.hostname));
}

const ERROR_BODY_LIMIT_BYTES = 65_536;

// Read only a bounded, contract-validated error envelope. The server message is never returned to the UI.
async function errorCode(response: Response): Promise<string | null> {
  if (response.headers.get("content-type")?.split(";")[0]?.trim().toLowerCase() !== "application/json" || !response.body) {
    await response.body?.cancel();
    return null;
  }
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let length = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      length += value.byteLength;
      if (length > ERROR_BODY_LIMIT_BYTES) {
        await reader.cancel();
        return null;
      }
      chunks.push(value);
    }
    const bytes = new Uint8Array(length);
    let offset = 0;
    for (const chunk of chunks) {
      bytes.set(chunk, offset);
      offset += chunk.byteLength;
    }
    return parseErrorsV3(JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(bytes))).error.code;
  } catch {
    return null;
  } finally {
    reader.releaseLock();
  }
}

// The host supplies the bearer and trusted API origin. Never persist either or log a response body.
async function post(input: Shared, path: string, body: object): Promise<SubmitResult> {
  let url: URL;
  try {
    url = new URL(path, input.baseUrl);
  } catch (error) {
    if (!(error instanceof TypeError)) throw error;
    return "unavailable";
  }
  if (!isSecureTarget(url) || !input.sessionToken) return "unavailable";
  try {
    const response = await fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json", Authorization: `Bearer ${input.sessionToken}` },
      body: JSON.stringify(body),
      credentials: "omit",
      cache: "no-store",
      redirect: "error",
      signal: input.signal,
    });
    if (response.status === 409 && path.startsWith("/v1/confirmations/")) {
      const code = await errorCode(response);
      return code === "CONFIRMATION_EXPIRED" ? "expired" : "unavailable";
    }
    const accepted = response.status === 202;
    await response.body?.cancel();
    return accepted ? "accepted" : "unavailable";
  } catch (error) {
    if (!(error instanceof TypeError || error instanceof DOMException)) throw error;
    return "unavailable";
  }
}

export async function submitClarification(input: Clarification): Promise<SubmitResult> {
  try {
    const card = parseOverlayCardV1ClarificationCard(input.card);
    const conversationRef = parseCommonV1InvestigationId(input.conversationRef);
    const state = cardExpiry(card.expires_at, Date.now());
    if (state !== "active") return state;
    if (!card.options.some((option) => option.option_id === input.optionId) && !(card.allow_none && input.optionId === card.none_option_id)) return "unavailable";
    const body = parseMessageRequestV1({ request_id: input.requestId, clarification_id: card.clarification_id, option_id: input.optionId });
    return await post(input, `/v1/conversations/${encodeURIComponent(conversationRef)}/messages`, body);
  } catch (error) {
    if (!(error instanceof ContractError)) throw error;
    return "unavailable";
  }
}

export async function submitConfirmation(input: Confirmation): Promise<SubmitResult> {
  try {
    const card = parseOverlayCardV1ConfirmationCard(input.card);
    const state = cardExpiry(card.expires_at, Date.now());
    if (state !== "active") return state;
    if (input.response !== "ACCEPT" && input.response !== "DECLINE") return "unavailable";
    const body = parseConfirmationAnswerForPath(card.confirmation_request_id, {
      request_id: card.confirmation_request_id, response: input.response,
    });
    return await post(input, `/v1/confirmations/${encodeURIComponent(card.confirmation_request_id)}`, body);
  } catch (error) {
    if (!(error instanceof ContractError)) throw error;
    return "unavailable";
  }
}
