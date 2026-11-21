import { parseCommonV1InvestigationId } from "./contract_types/common_v1.js";
import { parseInvestigationResponseV1, type InvestigationResponseV1 } from "./contract_types/customer.js";
import { RESPONSE_FRAME_LIMIT_BYTES } from "./transport-limits.js";

interface RefetchInput {
  baseUrl: string;
  investigationId: string;
  sessionToken: string;
  maxResponseBytes: number;
  signal?: AbortSignal;
}

const unavailable = () => new Error("This update is unavailable.");

export function isSecureInvestigationTarget(url: URL): boolean {
  if (url.username || url.password) return false;
  if (url.protocol === "https:") return true;
  return url.protocol === "http:" && (url.hostname === "localhost" || url.hostname === "[::1]" || /^127(?:\.\d{1,3}){3}$/u.test(url.hostname));
}

export async function boundedText(response: Response, limit: number): Promise<string> {
  if (!response.body) throw unavailable();
  const reader = response.body.getReader();
  const decoder = new TextDecoder("utf-8", { fatal: true });
  let text = "";
  let size = 0;
  try {
    for (;;) {
      const chunk = await reader.read();
      if (chunk.done) break;
      size += chunk.value.byteLength;
      if (size > limit) throw unavailable();
      text += decoder.decode(chunk.value, { stream: true });
    }
    return text + decoder.decode();
  } finally {
    try { await reader.cancel(); }
    finally { reader.releaseLock(); }
  }
}

/** Refetch only the core's current state. The host supplies its session and approved response bound. */
export async function fetchInvestigation(input: RefetchInput): Promise<InvestigationResponseV1> {
  try {
    const id = parseCommonV1InvestigationId(input.investigationId);
    const url = new URL(`/v1/investigations/${encodeURIComponent(id)}`, input.baseUrl);
    if (!isSecureInvestigationTarget(url) || !input.sessionToken || !Number.isSafeInteger(input.maxResponseBytes) || input.maxResponseBytes <= 0) throw unavailable();
    const response = await fetch(url, {
      method: "GET", headers: { Authorization: `Bearer ${input.sessionToken}` },
      credentials: "omit", cache: "no-store", redirect: "error", signal: input.signal,
    });
    const contentType = response.headers.get("content-type")?.split(";")[0]?.trim().toLowerCase();
    if (response.status !== 200 || contentType !== "application/json") {
      await response.body?.cancel();
      throw unavailable();
    }
    return parseInvestigationResponseV1(JSON.parse(await boundedText(response, Math.min(input.maxResponseBytes, RESPONSE_FRAME_LIMIT_BYTES))));
  } catch {
    throw unavailable();
  }
}
