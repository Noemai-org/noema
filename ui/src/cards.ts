import { createElement as h, useEffect, useRef, useState } from "react";
import { parseOverlayCardV1ClarificationCard, parseOverlayCardV1ConfirmationCard, type OverlayCardV1ClarificationCard, type OverlayCardV1ConfirmationCard } from "./contract_types/overlay_card_v1.js";
import { ContractError } from "./contract_types/_runtime.js";
import { cardExpiry } from "./card-expiry.js";

type Clock = () => number;
const clock: Clock = () => Date.now();
const expiredText = "This has changed. Let me check again.";

function useExpired(expiresAt: string, now: number | undefined): boolean {
  const expires = Date.parse(expiresAt);
  const [current, setCurrent] = useState(() => now ?? clock());
  useEffect(() => {
    if (now !== undefined) {
      setCurrent(now);
      return;
    }
    if (cardExpiry(expiresAt, clock()) === "unavailable") return;
    const remaining = expires - clock();
    if (!Number.isFinite(remaining) || remaining <= 0) {
      setCurrent(clock());
      return;
    }
    const timer = setTimeout(() => setCurrent(clock()), Math.min(remaining, 2_147_483_647));
    return () => clearTimeout(timer);
  }, [expires, now, current]);
  return cardExpiry(expiresAt, now ?? current) !== "active";
}

interface ClarificationProps {
  card: OverlayCardV1ClarificationCard;
  now?: number;
  onChoice?: (clarificationId: string, optionId: string, requestId: string) => void | "accepted" | "expired" | "unavailable" | Promise<void | "accepted" | "expired" | "unavailable">;
}

export function ClarificationCard(props: Omit<ClarificationProps, "card"> & { card: unknown }) {
  try {
    const card = parseOverlayCardV1ClarificationCard(props.card);
    return h(ClarificationView, { ...props, card, key: card.clarification_id });
  } catch (error) {
    if (!(error instanceof ContractError)) throw error;
    return h("p", { role: "status" }, "This card is unavailable.");
  }
}

function ClarificationView({ card, now, onChoice }: ClarificationProps) {
  const expired = useExpired(card.expires_at, now);
  const lock = useRef(false);
  const inFlight = useRef(false);
  const selection = useRef<{ optionId: string; requestId: string } | null>(null);
  const [locked, setLocked] = useState(false);
  const [retry, setRetry] = useState(false);
  const current = () => !expired && cardExpiry(card.expires_at, now ?? clock()) === "active";
  const submit = async () => {
    if (!onChoice || !selection.current || !current() || inFlight.current) return;
    inFlight.current = true;
    setRetry(false);
    try {
      const result = await onChoice(card.clarification_id, selection.current.optionId, selection.current.requestId);
      if (result === "unavailable") setRetry(true);
    } catch {
      setRetry(true);
    } finally {
      inFlight.current = false;
    }
  };
  const choose = (optionId: string) => {
    if (!onChoice || lock.current || !current()) return;
    const requestId = crypto.randomUUID();
    lock.current = true;
    selection.current = { optionId, requestId };
    setLocked(true);
    void submit();
  };
  return h("section", { "aria-label": "Clarification" },
    h("p", null, card.question),
    ...card.options.map((option) => h("button", {
      key: option.option_id,
      type: "button",
      disabled: expired || locked || !onChoice,
      onClick: () => choose(option.option_id),
    }, option.label)),
    h("button", {
      type: "button",
      disabled: expired || locked || !onChoice || !card.allow_none,
      onClick: () => choose(card.none_option_id),
    }, "None of these"),
    retry && !expired ? h("button", { type: "button", onClick: () => { if (retry) { setRetry(false); void submit(); } } }, "Try again") : null,
    expired ? h("p", { role: "status" }, expiredText) : null,
  );
}

interface ConfirmationProps {
  card: OverlayCardV1ConfirmationCard;
  now?: number;
  onResponse?: (confirmationId: string, response: "ACCEPT" | "DECLINE") => void | "accepted" | "expired" | "unavailable" | Promise<void | "accepted" | "expired" | "unavailable">;
}

export function ConfirmationCard(props: Omit<ConfirmationProps, "card"> & { card: unknown }) {
  try {
    const card = parseOverlayCardV1ConfirmationCard(props.card);
    return h(ConfirmationView, { ...props, card, key: card.confirmation_request_id });
  } catch (error) {
    if (!(error instanceof ContractError)) throw error;
    return h("p", { role: "status" }, "This card is unavailable.");
  }
}

function ConfirmationView({ card, now, onResponse }: ConfirmationProps) {
  const expired = useExpired(card.expires_at, now);
  const [showPolicy, setShowPolicy] = useState(false);
  const lock = useRef(false);
  const inFlight = useRef(false);
  const selected = useRef<"ACCEPT" | "DECLINE" | null>(null);
  const [locked, setLocked] = useState(false);
  const [retry, setRetry] = useState(false);
  const [serverExpired, setServerExpired] = useState(false);
  const isExpired = expired || serverExpired;
  const current = () => !isExpired && cardExpiry(card.expires_at, now ?? clock()) === "active";
  const submit = async () => {
    if (!onResponse || !selected.current || !current() || inFlight.current) return;
    inFlight.current = true;
    setRetry(false);
    try {
      const result = await onResponse(card.confirmation_request_id, selected.current);
      if (result === "unavailable") {
        selected.current = null;
        lock.current = false;
        setLocked(false);
        setRetry(true);
      } else if (result === "expired") {
        setRetry(false);
        setServerExpired(true);
      }
    } catch {
      selected.current = null;
      lock.current = false;
      setLocked(false);
      setRetry(true);
    } finally {
      inFlight.current = false;
    }
  };
  const response = (value: "ACCEPT" | "DECLINE") => () => {
    if (!onResponse || lock.current || !current()) return;
    selected.current = value;
    lock.current = true;
    setLocked(true);
    void submit();
  };
  return h("section", { "aria-label": "Confirmation" },
    h("p", null, card.action), h("p", null, card.target), h("p", null, card.evidence_summary),
    h("button", { type: "button", disabled: isExpired || locked || !onResponse, onClick: response("ACCEPT") }, "Accept"),
    h("button", { type: "button", disabled: expired || locked || !onResponse, onClick: response("DECLINE") }, "Decline"),
    h("button", { type: "button", disabled: isExpired, onClick: () => setShowPolicy(!showPolicy), "aria-expanded": showPolicy }, "Policy details"),
    showPolicy ? h("p", null, card.policy_version) : null,
    retry && !isExpired ? h("p", { role: "status" }, "The response could not be completed. Please try again.") : null,
    isExpired ? h("p", { role: "status" }, expiredText) : null,
  );
}
