import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { ClarificationCard, ConfirmationCard } from "../build/js/cards.js";
import { scanVisibleText } from "../build/js/privacy.js";

const fixture = async (name) => JSON.parse(await readFile(new URL(`../../contracts/fixtures/valid/overlay-card/${name}.json`, import.meta.url), "utf8"));
const render = (component, card, now) => renderToStaticMarkup(createElement(component, { card, now }));
// SYNTHETIC clock, before the fixture expiry; no wall-clock dependence.
const before = Date.parse("2026-11-13T14:00:00Z");
const after = Date.parse("2026-11-13T16:00:00Z");

test("REQ-UI-002 US-02: clarification presents only the server choices and none of these", async () => {
  const card = await fixture("clarification_four_options");
  const html = render(ClarificationCard, card, before);
  assert.deepEqual(scanVisibleText(html), []);
  assert.ok(html.includes(card.question));
  for (const option of card.options) assert.ok(html.includes(option.label));
  assert.match(html, /none of these/i);
  assert.match(html, /<button\b[^>]*\bdisabled(?:="")?[^>]*>None of these<\/button>/i);
  assert.doesNotMatch(html, /<input[^>]+type="text"/i);
  assert.ok(!html.includes(card.clarification_id));
});

test("REQ-UI-002 US-18: confirmation shows the supplied action, masked target and evidence", async () => {
  const card = await fixture("confirmation");
  const html = render(ConfirmationCard, card, before);
  assert.deepEqual(scanVisibleText(html), []);
  for (const value of [card.action, card.target, card.evidence_summary]) assert.ok(html.includes(value));
  assert.match(html, /accept/i);
  assert.match(html, /decline/i);
  assert.ok(!html.includes(card.confirmation_request_id));
  // PERD v1.25 §40.1 allows policy version on tap, not on initial render.
  assert.ok(!html.includes(card.policy_version));
});

test("REQ-UI-002: a device clock ahead of core time cannot disable a valid card", async () => {
  for (const [component, name, callback] of [[ClarificationCard, "clarification_four_options", "onChoice"], [ConfirmationCard, "confirmation", "onResponse"]]) {
    const card = { ...await fixture(name), expires_at: "2026-11-13T15:00:00Z" };
    const html = renderToStaticMarkup(createElement(component, { card, now: after, [callback]: () => "accepted" }));
    assert.deepEqual(scanVisibleText(html), []);
    assert.doesNotMatch(html, /This has changed\. Let me check again\./);
    const buttons = html.match(/<button\b[^>]*>/g) ?? [];
    assert.ok(buttons.some((button) => !/\bdisabled(?:=|\s|>)/.test(button)), name);
  }
});

test("REQ-UI-002: invalid cards are refused without rendering their data", async () => {
  const directory = new URL("../../contracts/fixtures/invalid/overlay-card/", import.meta.url);
  for (const name of ["label_with_full_account_number.json", "confirmation_target_unmasked.json", "clarification_with_binding_hash.json"]) {
    const card = JSON.parse(await readFile(new URL(name, directory), "utf8"));
    const component = card.card_type === "CONFIRMATION" ? ConfirmationCard : ClarificationCard;
    const html = render(component, card, before);
    assert.match(html, /This card is unavailable/i, name);
    assert.doesNotMatch(html, /<button\b/i, name);
    assert.deepEqual(scanVisibleText(html), [], name);
  }
});

test("REQ-UI-002: impossible calendar dates cannot enable a card", async () => {
  for (const [component, name, callback] of [[ClarificationCard, "clarification_four_options", "onChoice"], [ConfirmationCard, "confirmation", "onResponse"]]) {
    const original = await fixture(name);
    const valid = renderToStaticMarkup(createElement(component, { card: original, now: before, [callback]: () => {} }));
    assert.ok((valid.match(/<button\b[^>]*>/g) ?? []).some((button) => !/\bdisabled(?:=|\s|>)/.test(button)), name);
    assert.deepEqual(scanVisibleText(valid), []);
    const card = { ...original, expires_at: "2099-02-31T00:00:00Z" };
    const html = renderToStaticMarkup(createElement(component, { card, now: before, [callback]: () => {} }));
    const buttons = html.match(/<button\b[^>]*>/g) ?? [];
    assert.ok(buttons.every((button) => /\bdisabled(?:=|\s|>)/.test(button)), name);
    assert.deepEqual(scanVisibleText(html), []);
  }
});
