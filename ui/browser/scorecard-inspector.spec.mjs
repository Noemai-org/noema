import { test as base, expect } from "@playwright/test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { readFile } from "node:fs/promises";
import { ScorecardInspector } from "../build/js/scorecard-inspector.js";

const fixture = JSON.parse(await readFile("../contracts/fixtures/valid/scorecard-view/two_cases_clean.json", "utf8"));
const scanner = await readFile("build/js/scorecard-scan.js", "utf8");
const test = base.extend({ page: async ({ page }, use) => {
  try { await use(page); } finally {
    const findings = await page.evaluate(async (source) => {
      const url = URL.createObjectURL(new Blob([source], { type: "text/javascript" }));
      try { return (await import(url)).scanScorecardDom(document.body); }
      finally { URL.revokeObjectURL(url); }
    }, scanner);
    expect(findings).toEqual([]);
  }
} });

test("REQ-UI-004: synthetic scorecard shows Tier 0 before families with the exact bound", async ({ page }) => {
  await page.setContent(renderToStaticMarkup(createElement(ScorecardInspector, { scorecard: fixture })));
  await expect(page.getByRole("heading", { name: "Tier 0" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "By family" })).toBeVisible();
  await expect(page.getByText(String(fixture.tier0.gates[0].rate.exact_upper95), { exact: false }).first()).toBeVisible();
  const order = await page.getByRole("heading").allTextContents();
  expect(order.indexOf("Tier 0")).toBeLessThan(order.indexOf("By family"));
});
