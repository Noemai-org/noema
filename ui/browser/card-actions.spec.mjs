import { test as base, expect } from "@playwright/test";
import { build } from "esbuild";
import { createServer } from "node:http";
import { once } from "node:events";
import { networkInterfaces } from "node:os";
import { readFile } from "node:fs/promises";
import { scanVisibleText } from "../build/js/privacy.js";

// The actual card components, bundled only for this synthetic local browser
// harness. It is not the core and makes no API requests.
const js = (await build({ stdin: { contents: `
  import { createElement } from 'react';
  import { createRoot } from 'react-dom/client';
  import { ClarificationCard, ConfirmationCard } from './src/cards.ts';
  const root = createRoot(document.getElementById('app'));
  window.events = [];
  window.showClarification = card => root.render(createElement(ClarificationCard, { card, onChoice: (...args) => window.events.push(args) }));
  window.showRetryCard = card => root.render(createElement(ClarificationCard, { card, onChoice: async (...args) => {
    window.events.push(args);
    const response = await fetch('/retry-submit', { method: 'POST' });
    return response.ok ? 'accepted' : 'unavailable';
  } }));
  window.showConfirmation = card => root.render(createElement(ConfirmationCard, { card, onResponse: (...args) => window.events.push(args) }));
`, resolveDir: process.cwd(), loader: "js" }, bundle: true, format: "iife", platform: "browser", write: false, define: { "process.env.NODE_ENV": '"production"' } })).outputFiles[0].text;

const test = base.extend({ page: async ({ page }, use) => {
  try { await use(page); } finally {
    const values = await page.evaluate(() => {
      const root = document.body;
      const out = [root.textContent ?? ""];
      for (const el of [root, ...root.querySelectorAll("*")]) {
        for (const attribute of el.attributes) out.push(attribute.value);
        if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement || el instanceof HTMLSelectElement) out.push(el.value);
      }
      return out;
    });
    expect(values.flatMap(scanVisibleText)).toEqual([]);
  }
} });
let server, origin, attempts = 0;
test.beforeAll(async () => {
  const address = Object.values(networkInterfaces()).flat().find((x) => x?.internal && x.family === "IPv4")?.address;
  if (!address) throw new Error("Local test interface unavailable");
  server = createServer((req, res) => {
    if (req.url === "/") res.writeHead(200, { "content-type": "text/html" }).end('<!doctype html><div id="app"></div><script src="/cards.js"></script>');
    else if (req.url === "/cards.js") res.writeHead(200, { "content-type": "text/javascript" }).end(js);
    else if (req.url === "/retry-submit") res.writeHead(++attempts === 1 ? 503 : 202).end();
    else res.writeHead(404).end();
  });
  server.listen(0, address);
  await once(server, "listening");
  origin = `http://${address}:${server.address().port}`;
});
test.afterAll(async () => { server?.closeAllConnections(); if (server) await new Promise((resolve) => server.close(resolve)); });

async function card(name) {
  return { ...JSON.parse(await readFile(`../contracts/fixtures/valid/overlay-card/${name}.json`, "utf8")), expires_at: "2099-01-01T00:00:00Z" };
}

test("A491: retry after a real HTTP failure reuses the same request id", async ({ page }) => {
  attempts = 0;
  const value = await card("clarification_four_options");
  await page.goto(origin);
  await page.evaluate((input) => window.showRetryCard(input), value);
  await page.getByRole("button", { name: value.options[0].label }).click();
  await page.getByRole("button", { name: "Try again" }).click();
  await expect.poll(() => page.evaluate(() => window.events.length)).toBe(2);
  const events = await page.evaluate(() => window.events);
  expect(events[1]).toEqual(events[0]);
  await expect(page.getByRole("button", { name: "Try again" })).toHaveCount(0);
});

test("A275: none of these sends the id from the card", async ({ page }) => {
  const value = await card("clarification_four_options");
  await page.goto(origin);
  await page.evaluate((input) => window.showClarification(input), value);
  await page.getByRole("button", { name: "None of these" }).click();
  const events = await page.evaluate(() => window.events);
  expect(events[0][1]).toBe(value.none_option_id);
});

test("A491: clarification locks on first click and preserves its request identity", async ({ page }) => {
  const value = await card("clarification_four_options");
  await page.goto(origin);
  await page.evaluate((input) => window.showClarification(input), value);
  const option = page.getByRole("button", { name: value.options[0].label });
  await option.click();
  await expect(option).toBeDisabled();
  await page.evaluate((input) => window.showClarification(input), value);
  await expect(option).toBeDisabled();
  const events = await page.evaluate(() => window.events);
  expect(events).toHaveLength(1);
  expect(events[0].slice(0, 2)).toEqual([value.clarification_id, value.options[0].option_id]);
  expect(events[0][2]).toMatch(/^[0-9a-f-]{36}$/u);
});

test("A491 and PERD 40.1: confirmation locks after response and reveals policy only on tap", async ({ page }) => {
  const value = await card("confirmation");
  await page.goto(origin);
  await page.evaluate((input) => window.showConfirmation(input), value);
  await expect(page.getByText(value.policy_version, { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Policy details" }).click();
  await expect(page.getByText(value.policy_version, { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Accept" }).click();
  await expect(page.getByRole("button", { name: "Accept" })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Decline" })).toBeDisabled();
  expect(await page.evaluate(() => window.events)).toEqual([[value.confirmation_request_id, "ACCEPT"]]);
});
