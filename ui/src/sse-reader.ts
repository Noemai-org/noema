import { parseSseEventV1, type SseEventV1 } from "./contract_types/customer.js";

type Receiver = (event: SseEventV1) => void;
interface Frame { name: string; data: string[]; size: number }
const encoder = new TextEncoder();
export class StreamProtocolError extends Error {
  constructor() { super("This update is unavailable."); this.name = "StreamProtocolError"; }
}
export class StreamInterruptedError extends Error {
  constructor() { super("This update was interrupted."); this.name = "StreamInterruptedError"; }
}
const invalid = () => new StreamProtocolError();

function deliver(frame: Frame, receive: Receiver): void {
  if (frame.data.length === 0) return;
  let event: SseEventV1;
  try {
    event = parseSseEventV1(JSON.parse(frame.data.join("\n")));
    if (event.event !== frame.name) throw invalid();
  } catch {
    throw invalid();
  }
  receive(event);
}

function line(frame: Frame, text: string, newlineBytes: number, maxFrameBytes: number, receive: Receiver): void {
  frame.size += encoder.encode(text).byteLength + newlineBytes;
  if (frame.size > maxFrameBytes) throw invalid();
  if (text === "") {
    deliver(frame, receive);
    frame.name = "";
    frame.data = [];
    frame.size = 0;
    return;
  }
  if (text.startsWith(":")) return;
  const colon = text.indexOf(":");
  const field = colon < 0 ? text : text.slice(0, colon);
  let value = colon < 0 ? "" : text.slice(colon + 1);
  if (value.startsWith(" ")) value = value.slice(1);
  if (field === "event") frame.name = value;
  if (field === "data") frame.data.push(value);
}

/** Consume complete schema-validated events. The approved transport limit is supplied by the caller. */
export async function readEventStream(response: Response, receive: Receiver, maxFrameBytes: number): Promise<void> {
  const contentType = response.headers.get("content-type")?.split(";")[0]?.trim().toLowerCase();
  if (response.status !== 200 || contentType !== "text/event-stream" || !response.body || !Number.isSafeInteger(maxFrameBytes) || maxFrameBytes <= 0) {
    await response.body?.cancel();
    throw invalid();
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder("utf-8", { fatal: true });
  const frame: Frame = { name: "", data: [], size: 0 };
  let pending = "";
  try {
    for (;;) {
      const chunk = await reader.read();
      pending += chunk.done ? decoder.decode() : decoder.decode(chunk.value, { stream: true });
      for (;;) {
        const end = pending.search(/[\r\n]/u);
        if (end < 0 || (!chunk.done && pending[end] === "\r" && end === pending.length - 1)) break;
        const newline = pending[end] === "\r" && pending[end + 1] === "\n" ? 2 : 1;
        line(frame, pending.slice(0, end), newline, maxFrameBytes, receive);
        pending = pending.slice(end + newline);
      }
      if (frame.size + encoder.encode(pending).byteLength > maxFrameBytes) throw invalid();
      if (chunk.done) break;
    }
    if (pending !== "" || frame.size !== 0) throw new StreamInterruptedError();
  } finally {
    try { await reader.cancel(); }
    finally { reader.releaseLock(); }
  }
}
