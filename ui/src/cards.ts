import { createElement as h, useEffect, useState } from "react";
import { parseOverlayCardV1ClarificationCard, parseOverlayCardV1ConfirmationCard, type OverlayCardV1ClarificationCard, type OverlayCardV1ConfirmationCard } from "./contract_types/overlay_card_v1.js";
import { ContractError } from "./contract_types/_runtime.js";

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
    const remaining = expires - clock();
    if (!Number.isFinite(remaining) || remaining <= 0) {
      setCurrent(clock());
      return;
    }
    const timer = setTimeout(() => setCurrent(clock()), Math.min(remaining, 2_147_483_647));
    return () => clearTimeout(timer);
  }, [expires, now, current]);
  return !Number.isFinite(expires) || (now ?? current) >= expires;
}

interface ClarificationProps {
  card: OverlayCardV1ClarificationCard;
  now?: number;
  onChoice?: (clarificationId: string, optionId: string) => void;
}

export function ClarificationCard(props: Omit<ClarificationProps, "card"> & { card: unknown }) {
  try {
    return h(ClarificationView, { ...props, card: parseOverlayCardV1ClarificationCard(props.card) });
  } catch (error) {
    if (!(error instanceof ContractError)) throw error;
    return h("p", { role: "status" }, "This card is unavailable.");
  }
}

function ClarificationView({ card, now, onChoice }: ClarificationProps) {
  const expired = useExpired(card.expires_at, now);
  const current = () => !expired && (now ?? clock()) < Date.parse(card.expires_at);
  return h("section", { "aria-label": "Clarification" },
    h("p", null, card.question),
    ...card.options.map((option) => h("button", {
      key: option.option_id,
      type: "button",
      disabled: expired || !onChoice,
      onClick: () => { if (current()) onChoice?.(card.clarification_id, option.option_id); },
    }, option.label)),
    h("button", {
      type: "button",
      disabled: true,
    }, "None of these"),
    expired ? h("p", { role: "status" }, expiredText) : null,
  );
}

interface ConfirmationProps {
  card: OverlayCardV1ConfirmationCard;
  now?: number;
  onResponse?: (confirmationId: string, response: "ACCEPT" | "DECLINE") => void;
}

export function ConfirmationCard(props: Omit<ConfirmationProps, "card"> & { card: unknown }) {
  try {
    return h(ConfirmationView, { ...props, card: parseOverlayCardV1ConfirmationCard(props.card) });
  } catch (error) {
    if (!(error instanceof ContractError)) throw error;
    return h("p", { role: "status" }, "This card is unavailable.");
  }
}

function ConfirmationView({ card, now, onResponse }: ConfirmationProps) {
  const expired = useExpired(card.expires_at, now);
  const [showPolicy, setShowPolicy] = useState(false);
  const current = () => !expired && (now ?? clock()) < Date.parse(card.expires_at);
  const response = (value: "ACCEPT" | "DECLINE") => () => {
    if (current()) onResponse?.(card.confirmation_request_id, value);
  };
  return h("section", { "aria-label": "Confirmation" },
    h("p", null, card.action), h("p", null, card.target), h("p", null, card.evidence_summary),
    h("button", { type: "button", disabled: expired || !onResponse, onClick: response("ACCEPT") }, "Accept"),
    h("button", { type: "button", disabled: expired || !onResponse, onClick: response("DECLINE") }, "Decline"),
    h("button", { type: "button", disabled: expired, onClick: () => setShowPolicy(!showPolicy), "aria-expanded": showPolicy }, "Policy details"),
    showPolicy ? h("p", null, card.policy_version) : null,
    expired ? h("p", { role: "status" }, expiredText) : null,
  );
}
