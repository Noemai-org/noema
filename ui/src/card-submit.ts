import { ContractError } from "./contract_types/_runtime.js";
import { parseCommonV1InvestigationId } from "./contract_types/common_v1.js";
import { parseOverlayCardV1ClarificationCard, parseOverlayCardV1ConfirmationCard } from "./contract_types/overlay_card_v1.js";
import { parseMessageRequestV1 } from "./contract_types/customer.js";
import { cardExpiry } from "./card-expiry.js";

type SubmitResult = "accepted" | "expired" | "unavailable";
type Shared = { baseUrl: string; sessionToken: string; signal?: AbortSignal };
type Clarification = Shared & { conversationRef: string; card: unknown; optionId: string; requestId: string };
type Confirmation = Shared & { card: unknown; response: "ACCEPT" | "DECLINE" };

export function isSecureTarget(url: URL): boolean {
  if (url.username || url.password) return false;
  if (url.protocol === "https:") return true;
  return url.protocol === "http:" && (url.hostname === "localhost" || url.hostname === "[::1]" || /^127(?:\.\d{1,3}){3}$/u.test(url.hostname));
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
    if (!card.options.some((option) => option.option_id === input.optionId)) return "unavailable";
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
    return await post(input, `/v1/confirmations/${encodeURIComponent(card.confirmation_request_id)}`, {
      request_id: card.confirmation_request_id, response: input.response,
    });
  } catch (error) {
    if (!(error instanceof ContractError)) throw error;
    return "unavailable";
  }
}
