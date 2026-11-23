import { test as base, expect } from "@playwright/test";
import { createServer } from "node:http";
import { networkInterfaces } from "node:os";
import { once } from "node:events";
import { readFile } from "node:fs/promises";
import { scanVisibleText } from "../build/js/privacy.js";
import { assertAccessible } from "./flow-a11y.mjs";

// Synthetic local static server; not the core. No unlisted paths are served.
const files = new Map([["/", ["dist/index.html", "text/html"]], ["/tokens.css", ["dist/tokens.css", "text/css"]], ["/app.js", ["dist/app.js", "text/javascript"]]]);
const test = base.extend({
  page: async ({ page }, use) => {
    try { await use(page); } finally {
      try { const fields = await page.evaluate(() => {
        const root = document.body;
        const values = [root.textContent ?? ""];
        for (const element of [root, ...root.querySelectorAll("*")]) {
          for (const attribute of element.attributes) values.push(attribute.value);
          if (element instanceof HTMLInputElement || element instanceof HTMLTextAreaElement || element instanceof HTMLSelectElement) values.push(element.value);
        }
        return values;
      });
      expect(fields.flatMap(scanVisibleText)).toEqual([]); }
      finally { await assertAccessible(page); }
    }
  },
});

test("REQ-UI-001: the built overlay delivers its client module without browser errors", async ({ page }) => {
  const address = Object.values(networkInterfaces()).flat().find((entry) => entry?.internal && entry.family === "IPv4")?.address;
  expect(address).toBeTruthy();
  const server = createServer(async (request, response) => {
    const entry = files.get(request.url);
    if (!entry) { response.writeHead(404).end(); return; }
    try { const body = await readFile(entry[0]); response.writeHead(200, { "content-type": entry[1] }).end(body); }
    catch { response.writeHead(404).end(); }
  });
  server.listen(0, address);
  await once(server, "listening");
  const errors = [];
  const modules = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("response", (response) => { if (response.url().endsWith("/app.js")) modules.push(response.status()); });
  try {
    await page.goto(`http://${address}:${server.address().port}`);
    await expect(page.getByRole("textbox", { name: "Your message" })).toBeVisible();
    await expect(page.locator('script[type="module"][src="app.js"]')).toHaveCount(1);
    expect(modules).toEqual([200]);
    expect(errors).toEqual([]);
    await page.evaluate(() => {
      const script = document.createElement("script");
      script.textContent = "window.__inlinePolicyProbe = true";
      document.body.appendChild(script);
    });
    expect(await page.evaluate(() => window.__inlinePolicyProbe)).toBeUndefined();
  } finally {
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
  }
});
