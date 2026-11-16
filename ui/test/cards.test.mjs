import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { ClarificationCard, ConfirmationCard } from "../build/js/cards.js";

const fixture = async (name) => JSON.parse(await readFile(new URL(`../../contracts/fixtures/valid/overlay-card/${name}.json`, import.meta.url), "utf8"));
const render = (component, card, now) => renderToStaticMarkup(createElement(component, { card, now }));
// SYNTHETIC clock, before the fixture expiry; no wall-clock dependence.
const before = Date.parse("2026-11-13T14:00:00Z");
const after = Date.parse("2026-11-13T16:00:00Z");

test("REQ-UI-002 US-02: clarification presents only the server choices and none of these", async () => {
  const card = await fixture("clarification_four_options");
  const html = render(ClarificationCard, card, before);
  assert.ok(html.includes(card.question));
  for (const option of card.options) assert.ok(html.includes(option.label));
  assert.match(html, /none of these/i);
  assert.doesNotMatch(html, /<input[^>]+type="text"/i);
  assert.ok(!html.includes(card.clarification_id));
});

test("REQ-UI-002 US-18: confirmation shows the supplied action, masked target and evidence", async () => {
  const card = await fixture("confirmation");
  const html = render(ConfirmationCard, card, before);
  for (const value of [card.action, card.target, card.evidence_summary]) assert.ok(html.includes(value));
  assert.match(html, /accept/i);
  assert.match(html, /decline/i);
  assert.ok(!html.includes(card.confirmation_request_id));
});

test("REQ-UI-002: expired cards disable their controls and explain the change", async () => {
  for (const [component, name] of [[ClarificationCard, "clarification_four_options"], [ConfirmationCard, "confirmation"]]) {
    const html = render(component, await fixture(name), after);
    assert.ok(html.includes("This has changed. Let me check again."));
    const buttons = html.match(/<button\b[^>]*>/g) ?? [];
    assert.ok(buttons.length > 0);
    assert.ok(buttons.every((button) => /\bdisabled(?:=|\s|>)/.test(button)));
  }
});
