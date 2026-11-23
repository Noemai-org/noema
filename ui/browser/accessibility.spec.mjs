import { test as base, expect } from "@playwright/test";
import { assertAccessible } from "./flow-a11y.mjs";
import { readFile } from "node:fs/promises";

// Synthetic UI only. Axe and its rules run locally; no external script or host.
const test = base.extend({
  page: async ({ page }, use) => {
    try { await use(page); } finally {
      const scanner = await readFile("build/js/privacy.js", "utf8");
      const findings = await page.evaluate(async (source) => {
        const url = URL.createObjectURL(new Blob([source], { type: "text/javascript" }));
        try { return (await import(url)).scanDom(document.body); }
        finally { URL.revokeObjectURL(url); }
      }, scanner);
      expect(findings).toEqual([]);
    }
  },
});

test("REQ-UI-001: the customer overlay has no automated WCAG A/AA violations", async ({ page }) => {
  const markup = await readFile("dist/index.html", "utf8");
  const stylesheet = await readFile("dist/tokens.css", "utf8");
  // Isolated fixture uses injected test CSS/scanner; real CSP is tested on the served page.
  await page.setContent(markup.replace(/<meta http-equiv="Content-Security-Policy"[^>]*>/u, ""));
  await page.addStyleTag({ content: stylesheet });
  await expect.poll(() => page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue("--noema-color-text").trim())).not.toBe("");
  await expect(page.getByRole("textbox", { name: "Your message" })).toBeVisible();
  await assertAccessible(page);
});
