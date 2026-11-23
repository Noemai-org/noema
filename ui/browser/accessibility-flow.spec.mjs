import { test as base, expect } from "@playwright/test";
import { readFile } from "node:fs/promises";
import { assertAccessible } from "./flow-a11y.mjs";

// Synthetic local UI only; scan the DOM after this flow.
const test = base.extend({ page: async ({ page }, use) => {
  const requests = [];
  page.on("request", (request) => requests.push(request.url()));
  try { await use(page); } finally {
    expect(requests.filter((url) => /^https?:/u.test(url))).toEqual([]); // No HTTP API request or Record write; in-memory blob imports are test instrumentation.
    const scanner = await readFile("build/js/privacy.js", "utf8");
    const findings = await page.evaluate(async (source) => {
      const url = URL.createObjectURL(new Blob([source], { type: "text/javascript" }));
      try { return (await import(url)).scanDom(document.body); }
      finally { URL.revokeObjectURL(url); }
    }, scanner);
    expect(findings).toEqual([]);
  }
} });

test("REQ-UI-001: flow accessibility check rejects a missing image description", async ({ page }) => {
  const markup = await readFile("dist/index.html", "utf8");
  const stylesheet = await readFile("dist/tokens.css", "utf8");
  // Isolated fixture uses injected test CSS/scanner; real CSP is tested on the served page.
  await page.setContent(markup.replace(/<meta http-equiv="Content-Security-Policy"[^>]*>/u, ""));
  await page.addStyleTag({ content: stylesheet });
  await assertAccessible(page);
  await page.evaluate(() => { const img = document.createElement("img"); img.src = "data:image/gif;base64,R0lGODlhAQABAAAAACw="; document.body.append(img); });
  await expect(assertAccessible(page)).rejects.toThrow();
  await page.locator("img").evaluate((element) => element.remove());
});
