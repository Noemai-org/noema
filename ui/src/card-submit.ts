import { ContractError } from "./contract_types/_runtime.js";
import { parseCommonV1InvestigationId } from "./contract_types/common_v1.js";
import { parseOverlayCardV1ClarificationCard, parseOverlayCardV1ConfirmationCard } from "./contract_types/overlay_card_v1.js";

type SubmitResult = "accepted" | "expired" | "unavailable";
type Shared = { baseUrl: string; sessionToken: string; signal?: AbortSignal };
type Clarification = Shared & { conversationRef: string; card: unknown; optionId: string };
type Confirmation = Shared & { card: unknown; response: "ACCEPT" | "DECLINE" };

export function isSecureTarget(url: URL): boolean {
  if (url.username || url.password) return false;
  if (url.protocol === "https:") return true;
  return url.protocol === "http:" && (url.hostname === "localhost" || url.hostname === "[::1]" || /^127(?:\.\d{1,3}){3}$/u.test(url.hostname));
}

function expiry(value: string): "active" | "expired" | "unavailable" {
  const instant = Date.parse(value);
  if (!Number.isFinite(instant)) return "unavailable";
  const date = new Date(instant);
  const parts = [date.getUTCFullYear(), date.getUTCMonth() + 1, date.getUTCDate(), date.getUTCHours(), date.getUTCMinutes(), date.getUTCSeconds()];
  const expected = [value.slice(0, 4), value.slice(5, 7), value.slice(8, 10), value.slice(11, 13), value.slice(14, 16), value.slice(17, 19)].map(Number);
  if (parts.some((part, index) => part !== expected[index])) return "unavailable";
  return instant <= Date.now() ? "expired" : "active";
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
    const state = expiry(card.expires_at);
    if (state !== "active") return state;
    if (!card.options.some((option) => option.option_id === input.optionId)) return "unavailable";
    return await post(input, `/v1/conversations/${encodeURIComponent(conversationRef)}/messages`, {
      request_id: crypto.randomUUID(), clarification_id: card.clarification_id, option_id: input.optionId,
    });
  } catch (error) {
    if (!(error instanceof ContractError)) throw error;
    return "unavailable";
  }
}

export async function submitConfirmation(input: Confirmation): Promise<SubmitResult> {
  try {
    const card = parseOverlayCardV1ConfirmationCard(input.card);
    const state = expiry(card.expires_at);
    if (state !== "active") return state;
    if (input.response !== "ACCEPT" && input.response !== "DECLINE") return "unavailable";
    return await post(input, `/v1/confirmations/${encodeURIComponent(card.confirmation_request_id)}`, {
      request_id: crypto.randomUUID(), response: input.response,
    });
  } catch (error) {
    if (!(error instanceof ContractError)) throw error;
    return "unavailable";
  }
}
