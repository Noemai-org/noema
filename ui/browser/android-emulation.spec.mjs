import { test as base, expect, devices } from "@playwright/test";
import { readFile } from "node:fs/promises";

const markup = await readFile("dist/index.html", "utf8");
// Isolated fixture injects the test scanner and CSS; the served CSP is tested separately.
const fixtureMarkup = markup.replace(/<meta http-equiv="Content-Security-Policy"[^>]*>/u, "");
const stylesheet = await readFile("dist/tokens.css", "utf8");
const scanner = await readFile("build/js/privacy.js", "utf8");
const schema = JSON.parse(await readFile("../contracts/schemas/customer-answer.v1.json", "utf8"));
const answerPattern = schema.properties.found.items.pattern;

// Chromium's Pixel 5 profile is emulation, not an Android device or webview.
const test = base.extend({
  page: async ({ page }, use) => {
    const requests = [];
    page.on("request", (request) => requests.push(request.url()));
    try { await use(page); } finally {
      expect(requests.filter((url) => /^https?:/u.test(url))).toEqual([]); // No HTTP API request or Record write; in-memory blob imports are test instrumentation.
      const findings = await page.evaluate(async (source) => {
        const url = URL.createObjectURL(new Blob([source], { type: "text/javascript" }));
        try { return (await import(url)).scanDom(document.body); }
        finally { URL.revokeObjectURL(url); }
      }, scanner);
      expect(findings).toEqual([]);
    }
  },
});
test.use({ ...devices["Pixel 5"] });

test("REQ-UI-001: the static overlay fits the emulated Android viewport", async ({ page }) => {
  await page.setContent(fixtureMarkup);
  await page.addStyleTag({ content: stylesheet });
  await expect(page.getByRole("textbox", { name: "Your message" })).toBeVisible();
  await expect(page.getByRole("region", { name: "Messages" })).toBeEmpty();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
});

test("REQ-UI-001: emulated Android action targets meet the existing touch token in both dimensions", async ({ page }) => {
  await page.setContent(fixtureMarkup);
  await page.addStyleTag({ content: stylesheet });
  for (const name of ["Send", "Push to talk"]) {
    const button = page.getByRole("button", { name });
    await expect(button).toBeVisible();
    await expect(button).toBeDisabled();
    const box = await button.boundingBox();
    const minimum = await button.evaluate((element) => parseFloat(getComputedStyle(element).minHeight));
    expect(box).not.toBeNull();
    expect(box.width).toBeGreaterThanOrEqual(minimum);
    expect(box.height).toBeGreaterThanOrEqual(minimum);
  }
});

test("REQ-UI-001: the Android DOM scan refuses synthetic runs rejected by the shared answer contract", async ({ page }) => {
  await page.setContent(fixtureMarkup);
  await page.addStyleTag({ content: stylesheet });
  const results = await page.evaluate(async ({ source, pattern }) => {
    const url = URL.createObjectURL(new Blob([source], { type: "text/javascript" }));
    try {
      const { scanDom } = await import(url);
      const contract = new RegExp(pattern, "u");
      return ["1234\u200e5", "[1234][5]", "1234\u00b75"].map((sample) => {
        const root = document.createElement("div");
        root.textContent = sample;
        return { contractRejects: !contract.test(sample), scannerRejects: scanDom(root).includes("digit-run") };
      });
    } finally { URL.revokeObjectURL(url); }
  }, { source: scanner, pattern: answerPattern });
  for (const result of results) expect(result).toEqual({ contractRejects: true, scannerRejects: true });
});
