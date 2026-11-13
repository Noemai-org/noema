import { createElement as h } from "react";

// Static shell only. Product messages, progress and actions await server contracts.
export function Overlay() {
  return h("main", { className: "overlay", "aria-label": "Noema" },
    h("h1", null, "Noema"),
    h("p", { className: "notice" }, "Messaging is not yet available."),
    h("section", { "aria-label": "Messages", "aria-live": "polite" }),
    h("label", { htmlFor: "message" }, "Your message"),
    h("textarea", { id: "message", name: "message", rows: 3 }),
    h("div", { className: "actions" },
      h("button", { type: "button", disabled: true }, "Send"),
      h("button", { type: "button", disabled: true }, "Push to talk")));
}
