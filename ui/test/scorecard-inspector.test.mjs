import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { ScorecardInspector } from "../build/js/scorecard-inspector.js";
import { scanVisibleText } from "../build/js/privacy.js";

const fixture = async (name) => JSON.parse(await readFile(new URL(`../../contracts/fixtures/valid/scorecard-view/${name}.json`, import.meta.url), "utf8"));
const render = (scorecard) => renderToStaticMarkup(createElement(ScorecardInspector, { scorecard }));

test("REQ-UI-004: tier zero appears before other metrics for a synthetic scorecard", async () => {
  const html = render(await fixture("two_cases_clean"));
  const tier0 = html.indexOf("Tier 0");
  const tier1 = html.indexOf("Tier 1");
  assert.ok(tier0 >= 0 && tier1 > tier0);
  assert.deepEqual(scanVisibleText(html), []);
});

test("REQ-UI-004: benchmark evidence retains the supplied exact bound without privacy findings", async () => {
  const scorecard = await fixture("two_cases_clean");
  const html = render(scorecard);
  assert.ok(html.includes(String(scorecard.tier0.gates[0].rate.exact_upper95)));
  assert.deepEqual(scanVisibleText(html), []);
});

test("REQ-UI-004 INV-22: an injected case list is refused without rendering it", async () => {
  const scorecard = await fixture("two_cases_clean");
  const html = render({ ...scorecard, failure_list: ["BEN-AMB-0001"] });
  assert.ok(!html.includes("BEN-AMB-0001"));
  assert.match(html, /unavailable/i);
  assert.deepEqual(scanVisibleText(html), []);
});
