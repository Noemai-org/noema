import { createElement as h, useRef, useState } from "react";
import { parseOverlayCardV1ClarificationCard, parseOverlayCardV1ConfirmationCard, type OverlayCardV1ClarificationCard, type OverlayCardV1ConfirmationCard } from "./contract_types/overlay_card_v1.js";
import { ContractError } from "./contract_types/_runtime.js";
import { isCanonicalInstant } from "./card-expiry.js";

const expiredText = "This has changed. Let me check again.";

function validCardExpiry(expiresAt: string): boolean {
  return isCanonicalInstant(expiresAt);
}

interface ClarificationProps {
  card: OverlayCardV1ClarificationCard;
  onChoice?: (clarificationId: string, optionId: string, requestId: string) => void | "accepted" | "expired" | "unavailable" | Promise<void | "accepted" | "expired" | "unavailable">;
}

export function ClarificationCard(props: Omit<ClarificationProps, "card"> & { card: unknown }) {
  try {
    const card = parseOverlayCardV1ClarificationCard(props.card);
    if (!validCardExpiry(card.expires_at)) return h("p", { role: "status" }, "This card is unavailable.");
    return h(ClarificationView, { ...props, card, key: card.clarification_id });
  } catch (error) {
    if (!(error instanceof ContractError)) throw error;
    return h("p", { role: "status" }, "This card is unavailable.");
  }
}

function ClarificationView({ card, onChoice }: ClarificationProps) {
  const [serverExpired, setServerExpired] = useState(false);
  const lock = useRef(false);
  const inFlight = useRef(false);
  const selection = useRef<{ optionId: string; requestId: string } | null>(null);
  const [locked, setLocked] = useState(false);
  const [retry, setRetry] = useState(false);
  const current = () => !serverExpired;
  const submit = async () => {
    if (!onChoice || !selection.current || !current() || inFlight.current) return;
    inFlight.current = true;
    setRetry(false);
    try {
      const result = await onChoice(card.clarification_id, selection.current.optionId, selection.current.requestId);
      if (result === "unavailable") setRetry(true);
      else if (result === "expired") { setRetry(false); setServerExpired(true); }
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
      disabled: serverExpired || locked || !onChoice,
      onClick: () => choose(option.option_id),
    }, option.label)),
    h("button", {
      type: "button",
      disabled: serverExpired || locked || !onChoice || !card.allow_none,
      onClick: () => choose(card.none_option_id),
    }, "None of these"),
    retry && !serverExpired ? h("button", { type: "button", onClick: () => { if (retry) { setRetry(false); void submit(); } } }, "Try again") : null,
    serverExpired ? h("p", { role: "status" }, expiredText) : null,
  );
}

interface ConfirmationProps {
  card: OverlayCardV1ConfirmationCard;
  onResponse?: (confirmationId: string, response: "ACCEPT" | "DECLINE") => void | "accepted" | "expired" | "unavailable" | Promise<void | "accepted" | "expired" | "unavailable">;
}

export function ConfirmationCard(props: Omit<ConfirmationProps, "card"> & { card: unknown }) {
  try {
    const card = parseOverlayCardV1ConfirmationCard(props.card);
    if (!validCardExpiry(card.expires_at)) return h("p", { role: "status" }, "This card is unavailable.");
    return h(ConfirmationView, { ...props, card, key: card.confirmation_request_id });
  } catch (error) {
    if (!(error instanceof ContractError)) throw error;
    return h("p", { role: "status" }, "This card is unavailable.");
  }
}

function ConfirmationView({ card, onResponse }: ConfirmationProps) {
  const [showPolicy, setShowPolicy] = useState(false);
  const lock = useRef(false);
  const inFlight = useRef(false);
  const selected = useRef<"ACCEPT" | "DECLINE" | null>(null);
  const [locked, setLocked] = useState(false);
  const [retry, setRetry] = useState(false);
  const [serverExpired, setServerExpired] = useState(false);
  const isExpired = serverExpired;
  const current = () => !serverExpired;
  const submit = async () => {
    if (!onResponse || !selected.current || !current() || inFlight.current) return;
    inFlight.current = true;
    setRetry(false);
    try {
      const result = await onResponse(card.confirmation_request_id, selected.current);
      if (result === "unavailable") {
        setRetry(true);
      } else if (result === "expired") {
        setRetry(false);
        setServerExpired(true);
      }
    } catch {
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
    h("button", { type: "button", disabled: isExpired || locked || !onResponse, onClick: response("DECLINE") }, "Decline"),
    h("button", { type: "button", disabled: isExpired, onClick: () => setShowPolicy(!showPolicy), "aria-expanded": showPolicy }, "Policy details"),
    showPolicy ? h("p", null, card.policy_version) : null,
    retry && !isExpired ? h("p", { role: "status" }, "I could not confirm whether your response was recorded. Retry the same response.") : null,
    retry && !isExpired ? h("button", { type: "button", onClick: () => { if (retry) { setRetry(false); void submit(); } } }, "Try again") : null,
    isExpired ? h("p", { role: "status" }, expiredText) : null,
  );
}
