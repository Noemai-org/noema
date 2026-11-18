import { test as base, expect } from "@playwright/test";
import { readFile } from "node:fs/promises";

const scanner = await readFile("build/js/scorecard-scan.js", "utf8");
async function scan(page) {
  return page.evaluate(async (source) => {
    const url = URL.createObjectURL(new Blob([source], { type: "text/javascript" }));
    try { return (await import(url)).scanScorecardDom(document.body); }
    finally { URL.revokeObjectURL(url); }
  }, scanner);
}
const test = base.extend({ page: async ({ page }, use) => {
  try { await use(page); } finally { expect(await scan(page)).toEqual([]); }
} });

test("A311: inspector scan catches case IDs in hidden text, attributes and form values", async ({ page }) => {
  await page.setContent('<div>0.776394</div><span hidden>BEN-AMB-0001</span><input aria-label="BDC-ABC-123456"><input>');
  await page.locator('input').last().fill('BEN-TEST-1234');
  const findings = await scan(page);
  expect(findings.filter((x) => x === "case-id").length).toBeGreaterThanOrEqual(3);
  await page.setContent('<div>0.776394</div>');
});
