import { test as base, expect } from "@playwright/test";
import { readFile } from "node:fs/promises";
const markup = await readFile("dist/index.html", "utf8");
const stylesheet = await readFile("dist/tokens.css", "utf8");
const scanner = await readFile("build/js/privacy.js", "utf8");

async function load(page) {
  await page.setContent(markup);
  await page.addStyleTag({ content: stylesheet });
}

async function scan(page, synthetic = false) {
  return page.evaluate(async ({ source, synthetic }) => {
    const moduleUrl = URL.createObjectURL(new Blob([source], { type: "text/javascript" }));
    try {
      const { scanDom } = await import(moduleUrl);
      const root = synthetic ? document.createElement("div") : document.body;
      if (synthetic) root.textContent = "Model confidence: 0.9; account 12345";
      return scanDom(root);
    } finally { URL.revokeObjectURL(moduleUrl); }
  }, { source: scanner, synthetic });
}

// Every browser flow uses this fixture. Test data is synthetic.
const test = base.extend({
  page: async ({ page }, use) => {
    try { await use(page); } finally { expect(await scan(page)).toEqual([]); }
  },
});

test("REQ-UI-001: scanner checks attribute and input value leakage", async ({ page }) => {
  await load(page);
  const findings = await page.evaluate(async (source) => {
    const moduleUrl = URL.createObjectURL(new Blob([source], { type: "text/javascript" }));
    try {
      const { scanDom } = await import(moduleUrl);
      const root = document.createElement("div");
      root.innerHTML = '<input aria-label="1-2-3-4-5"><input>';
      root.querySelectorAll("input")[1].value = "12 34 5";
      return scanDom(root);
    } finally { URL.revokeObjectURL(moduleUrl); }
  }, scanner);
  expect(findings.filter((item) => item === "digit-run").length).toBeGreaterThanOrEqual(2);
});

test("REQ-UI-001: the skeleton shows no fabricated state or answer", async ({ page }) => {
  await load(page);
  await expect(page.getByRole("textbox", { name: "Your message" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Send" })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Push to talk" })).toBeDisabled();
  await expect(page.getByRole("region", { name: "Messages" })).toBeEmpty();
  await expect(page.getByText(/thinking|clarifying|confirming|answering/i)).toHaveCount(0);
});

test("REQ-UI-001: the DOM scanner catches synthetic leakage", async ({ page }) => {
  await load(page);
  const findings = await scan(page, true);
  expect(findings).toEqual(expect.arrayContaining(["digit-run", "model-confidence"]));
});
