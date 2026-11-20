import { test as base, expect } from "@playwright/test";
import { readFile } from "node:fs/promises";
import ts from "typescript";
import { assertAccessible } from "../browser/flow-a11y.mjs";

// Compile the existing test-only scanner in memory. The stack step runs npm ci,
// not npm run build, and must not rely on a stale build/ directory.
const source = await readFile(new URL("../src/privacy.ts", import.meta.url), "utf8");
const js = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } }).outputText;
const { scanVisibleText } = await import(`data:text/javascript;base64,${Buffer.from(js).toString("base64")}`);

const test = base.extend({ page: async ({ page }, use) => {
  try { await use(page); } finally {
    try {
      const fields = await page.evaluate(() => {
        const root = document.body;
        if (!root) return [];
        const values = [root.textContent ?? ""];
        for (const element of [root, ...root.querySelectorAll("*")]) {
          for (const attribute of element.attributes) values.push(attribute.value);
          if (element instanceof HTMLInputElement || element instanceof HTMLTextAreaElement || element instanceof HTMLSelectElement) values.push(element.value);
        }
        return values;
      });
      expect(fields.flatMap(scanVisibleText)).toEqual([]);
    } finally { await assertAccessible(page); }
  }
} });

test("A388: deployed stack overlay exposes its message control without inventing an answer", async ({ page }) => {
  const response = await page.goto("/", { waitUntil: "domcontentloaded" });
  expect(response?.status()).toBe(200);
  await expect(page.getByRole("textbox", { name: "Your message" })).toBeVisible();
  await expect(page.getByRole("region", { name: "Messages" })).toBeEmpty();
});
