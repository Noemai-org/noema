import test from "node:test";
import assert from "node:assert/strict";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { Overlay } from "../build/js/overlay.js";

test("REQ-UI-001: skeleton accepts text but cannot invent a response", () => {
  const html = renderToStaticMarkup(createElement(Overlay));
  assert.match(html, /<textarea/u);
  assert.match(html, /disabled=""[^>]*>Send/u);
  assert.match(html, /disabled=""[^>]*>Push to talk/u);
  assert.match(html, /aria-label="Messages"/u);
  assert.doesNotMatch(html, /Thinking|Clarifying|Confirming|Answering/u);
});
