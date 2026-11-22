import { createElement, useEffect, useRef, useState } from "react";
import { hydrateRoot } from "react-dom/client";
import { Overlay } from "./overlay.js";
import { pollInvestigation } from "./polling.js";
import { progressCopy } from "./progress-copy.js";
import { CustomerAnswer } from "./customer-answer.js";
import type { InvestigationResponseV1 } from "./contract_types/customer.js";
import { RESPONSE_FRAME_LIMIT_BYTES } from "./transport-limits.js";
import { startConversationAndPoll } from "./conversation-start.js";

const app = document.getElementById("app");
const root = app ? hydrateRoot(app, createElement(Overlay)) : undefined;

type HostContext = { baseUrl: string; sessionToken: string; investigationId?: string; maxPollingMs: number; maxResponseBytes?: number };

function LiveOverlay({ context }: { context: HostContext }) {
  const [state, setState] = useState<InvestigationResponseV1>();
  const [lost, setLost] = useState(false);
  const [text, setText] = useState("");
  const [started, setStarted] = useState(false);
  const sending = useRef(false);
  const request = useRef<AbortController | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    request.current = controller;
    setState(undefined);
    setLost(false);
    if (!context.investigationId) return () => controller.abort();
    void pollInvestigation({
      ...context,
      investigationId: context.investigationId,
      maxResponseBytes: Math.min(context.maxResponseBytes ?? RESPONSE_FRAME_LIMIT_BYTES, RESPONSE_FRAME_LIMIT_BYTES),
      signal: controller.signal,
      onState: setState,
    }).then((failure) => { if (!controller.signal.aborted && failure) setLost(true); })
      .catch(() => { if (!controller.signal.aborted) setLost(true); });
    return () => controller.abort();
  }, [context]);
  const send = () => {
    if (sending.current || context.investigationId || !text.trim()) return;
    sending.current = true;
    setStarted(true);
    setLost(false);
    void startConversationAndPoll({ ...context, request: { request_id: crypto.randomUUID(), text }, signal: request.current?.signal, onState: setState })
      .then((failure) => { if (!request.current?.signal.aborted && failure) setLost(true); })
      .catch(() => { if (!request.current?.signal.aborted) setLost(true); });
  };
  return createElement("main", { className: "overlay", "aria-label": "Noema" },
    createElement("h1", null, "Noema"),
    createElement("section", { "aria-label": "Messages", "aria-live": "polite" },
      lost ? createElement("p", { role: "status" }, "Connection lost.") : null,
      state ? createElement("p", { role: "status" }, progressCopy(state.state)) : null,
      state?.state === "answered" && state.last_answer ? createElement(CustomerAnswer, { answer: state.last_answer }) : null),
    createElement("label", { htmlFor: "message" }, "Your message"),
    createElement("textarea", { id: "message", name: "message", rows: 3, value: text, onChange: (event: React.ChangeEvent<HTMLTextAreaElement>) => setText(event.currentTarget.value) }),
    createElement("div", { className: "actions" },
      createElement("button", { type: "button", disabled: Boolean(context.investigationId) || started || !text.trim(), onClick: send }, "Send"),
      createElement("button", { type: "button", disabled: true }, "Push to talk")));
}

// Host provides the session and existing investigation; POST is not wired yet.
export function configureOverlay(context: HostContext): void {
  if (!root) throw new Error("The overlay is unavailable.");
  root.render(createElement(LiveOverlay, { context: { ...context } }));
}
