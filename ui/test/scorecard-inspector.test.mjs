import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { ScorecardInspector } from "../build/js/scorecard-inspector.js";
import { scanScorecardText } from "../build/js/scorecard-scan.js";

const fixture = async (name) => JSON.parse(await readFile(new URL(`../../contracts/fixtures/valid/scorecard-view/${name}.json`, import.meta.url), "utf8"));
const render = (scorecard) => renderToStaticMarkup(createElement(ScorecardInspector, { scorecard }));

test("REQ-UI-004: tier zero appears before other metrics for a synthetic scorecard", async () => {
  const html = render(await fixture("two_cases_clean"));
  const tier0 = html.indexOf("Tier 0");
  const tier1 = html.indexOf("Tier 1");
  assert.ok(tier0 >= 0 && tier1 > tier0);
  assert.deepEqual(scanScorecardText(html), []);
});

test("REQ-UI-004: benchmark evidence retains the supplied exact bound without privacy findings", async () => {
  const scorecard = await fixture("two_cases_clean");
  const html = render(scorecard);
  assert.ok(html.includes(String(scorecard.tier0.gates[0].rate.exact_upper95)));
  assert.deepEqual(scanScorecardText(html), []);
});

test("REQ-UI-004: all supplied family counts and tier zero gates are shown without invented results", async () => {
  const scorecard = await fixture("two_cases_clean");
  const html = render(scorecard);
  for (const gate of scorecard.tier0.gates) assert.ok(html.includes(gate.gate.toLowerCase().replaceAll("_", " ")));
  for (const family of Object.keys(scorecard.by_family)) assert.ok(html.includes(family));
  assert.ok(html.indexOf("Tier 0") < html.indexOf("By family"));
  assert.deepEqual(scanScorecardText(html), []);
});

test("REQ-UI-004 INV-22: an injected case list is refused without rendering it", async () => {
  const scorecard = await fixture("two_cases_clean");
  const html = render({ ...scorecard, failure_list: ["BEN-AMB-0001"] });
  assert.ok(!html.includes("BEN-AMB-0001"));
  assert.match(html, /unavailable/i);
  assert.deepEqual(scanScorecardText(html), []);
});

test("A446: an inexact JSON integer cannot be displayed as an exact family count", async () => {
  for (const field of ["n", "gate_trips"]) {
    const scorecard = await fixture("two_cases_clean");
    scorecard.by_family.B1[field] = JSON.parse("9007199254740993");
    const html = render(scorecard);
    assert.match(html, /unavailable/i);
    assert.ok(!html.includes("9007199254740992"));
    assert.deepEqual(scanScorecardText(html), []);
  }
  const safe = await fixture("two_cases_clean");
  safe.by_family.B1.n = Number.MAX_SAFE_INTEGER;
  assert.ok(render(safe).includes("9007199254740991"));
});
