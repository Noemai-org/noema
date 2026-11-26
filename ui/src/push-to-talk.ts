import { createElement as h, useEffect, useRef, useState } from "react";
import { encodePcm16Mono, startPcmCapture } from "./pcm-capture.js";

export { encodePcm16Mono };
export const MAX_PUSH_TO_TALK_RECORDING_MS = 30_000;
type Props = { onPcm?: (bytes: Uint8Array) => void; maxRecordingMs?: number };

// No sink means no capture; the host integrates voice only when it exists.
export function PushToTalk({ onPcm, maxRecordingMs = MAX_PUSH_TO_TALK_RECORDING_MS }: Props) {
  const recordingLimitMs = Number.isSafeInteger(maxRecordingMs) && maxRecordingMs > 0
    ? Math.min(maxRecordingMs, MAX_PUSH_TO_TALK_RECORDING_MS)
    : 0;
  const controller = useRef<AbortController | null>(null);
  const [active, setActive] = useState(false);
  const [unavailable, setUnavailable] = useState(false);
  const end = () => {
    controller.current?.abort();
    controller.current = null;
    setActive(false);
  };
  useEffect(() => {
    const stopForWindowLoss = () => {
      controller.current?.abort();
      controller.current = null;
      setActive(false);
    };
    const stopWhenHidden = () => {
      if (document.visibilityState === "hidden") stopForWindowLoss();
    };
    window.addEventListener("blur", stopForWindowLoss);
    document.addEventListener("visibilitychange", stopWhenHidden);
    return () => {
      window.removeEventListener("blur", stopForWindowLoss);
      document.removeEventListener("visibilitychange", stopWhenHidden);
      controller.current?.abort();
      controller.current = null;
    };
  }, []);
  const begin = async () => {
    if (!onPcm || recordingLimitMs <= 0 || controller.current) return;
    const current = new AbortController();
    controller.current = current;
    setUnavailable(false);
    try {
      await startPcmCapture(onPcm, current.signal, () => {
        if (controller.current === current) { controller.current = null; setActive(false); setUnavailable(true); }
      }, recordingLimitMs);
      if (!current.signal.aborted && controller.current === current) setActive(true);
    } catch {
      if (!current.signal.aborted && controller.current === current) { controller.current = null; setUnavailable(true); }
    }
  };
  return h("div", null,
    h("button", {
      type: "button", disabled: !onPcm || typeof maxRecordingMs !== "number" || !Number.isSafeInteger(maxRecordingMs) || maxRecordingMs <= 0 || maxRecordingMs > 2_147_483_647, "aria-pressed": active,
      onPointerDown: (event: React.PointerEvent<HTMLButtonElement>) => {
        event.preventDefault();
        event.currentTarget.setPointerCapture(event.pointerId);
        void begin();
      },
      onPointerUp: end, onPointerCancel: end, onLostPointerCapture: end, onBlur: end,
      onKeyDown: (event: React.KeyboardEvent<HTMLButtonElement>) => {
        if ((event.key === " " || event.key === "Enter") && !event.repeat) { event.preventDefault(); void begin(); }
      },
      onKeyUp: (event: React.KeyboardEvent<HTMLButtonElement>) => {
        if (event.key === " " || event.key === "Enter") { event.preventDefault(); end(); }
      },
    }, "Push to talk"),
    active ? h("p", { role: "status" }, "Recording") : null,
    unavailable ? h("p", { role: "status" }, "Voice is unavailable.") : null,
  );
}
