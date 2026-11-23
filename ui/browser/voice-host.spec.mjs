import { test as base, expect } from "@playwright/test";
import { build } from "esbuild";
import { createServer } from "node:http";
import { once } from "node:events";
import { networkInterfaces } from "node:os";
import { existsSync } from "node:fs";
import { scanVisibleText } from "../build/js/privacy.js";

// Synthetic host and synthetic microphone; no audio leaves the browser.
const bundle = (await build({ stdin: { contents: `
  import { createElement } from 'react';
  import { createRoot } from 'react-dom/client';
  import { PushToTalk } from './src/push-to-talk.ts';
  window.frameSizes = [];
  createRoot(document.getElementById('app')).render(createElement(PushToTalk, {
    maxRecordingMs: 20000,
    onPcm: frame => window.frameSizes.push(frame.byteLength)
  }));
`, resolveDir: process.cwd(), loader: "js" }, bundle: true, format: "iife", platform: "browser", write: false, define: { "process.env.NODE_ENV": '"production"' } })).outputFiles[0].text;
const test = base.extend({ page: async ({ page }, use) => {
  try { await use(page); } finally {
    const values = await page.evaluate(() => [document.body.textContent ?? "", ...Array.from(document.querySelectorAll("*"), el => Array.from(el.attributes, a => a.value)).flat()]);
    expect(values.flatMap(scanVisibleText)).toEqual([]);
  }
} });
const browser = ["/usr/bin/google-chrome", "/usr/bin/chromium"].find(existsSync);
test.use({ launchOptions: { executablePath: browser, args: ["--use-fake-device-for-media-stream"] } });
test("A515: host supplies the limit and synthetic sink receives frames while Recording is visible", async ({ page, context }) => {
  const address = Object.values(networkInterfaces()).flat().find(x => x?.internal && x.family === "IPv4")?.address;
  if (!address) throw new Error("Local test interface unavailable");
  const server = createServer((req, res) => {
    if (req.url === "/voice.js") res.writeHead(200, { "content-type": "text/javascript" }).end(bundle);
    else res.writeHead(200, { "content-type": "text/html" }).end('<!doctype html><div id="app"></div><script src="/voice.js"></script>');
  });
  server.listen(0, address); await once(server, "listening");
  const origin = `http://${address}:${server.address().port}`;
  try {
    await context.grantPermissions(["microphone"], { origin });
    await page.goto(origin);
    const button = page.getByRole("button", { name: "Push to talk" });
    await button.hover(); await page.mouse.down();
    await expect(page.getByRole("status")).toHaveText("Recording");
    await expect.poll(() => page.evaluate(() => window.frameSizes.length)).toBeGreaterThan(0);
    const sizes = await page.evaluate(() => window.frameSizes);
    expect(sizes.every(size => size > 0 && size <= 32000 && size % 2 === 0)).toBe(true);
    await page.mouse.up();
    await expect(page.getByText("Recording", { exact: true })).toHaveCount(0);
  } finally { server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); }
});

test("A516: capture stops and tracks close when the browser window loses focus", async ({ page, context }) => {
  const address = Object.values(networkInterfaces()).flat().find(x => x?.internal && x.family === "IPv4")?.address;
  if (!address) throw new Error("Local test interface unavailable");
  const server = createServer((_req, res) => res.writeHead(200, { "content-type": "text/html" }).end('<!doctype html><div id="app"></div><script src="/voice.js"></script>'));
  server.on("request", (req, res) => {
    if (req.url === "/voice.js") res.writeHead(200, { "content-type": "text/javascript" }).end(bundle);
  });
  server.listen(0, address); await once(server, "listening");
  const origin = `http://${address}:${server.address().port}`;
  try {
    await context.grantPermissions(["microphone"], { origin });
    await page.addInitScript(() => {
      const original = navigator.mediaDevices.getUserMedia.bind(navigator.mediaDevices);
      navigator.mediaDevices.getUserMedia = async (constraints) => {
        const stream = await original(constraints);
        (window as any).captureTracks = stream.getTracks();
        return stream;
      };
    });
    await page.goto(origin);
    const button = page.getByRole("button", { name: "Push to talk" });
    await button.hover(); await page.mouse.down();
    await expect(page.getByRole("status")).toHaveText("Recording");
    await expect.poll(() => page.evaluate(() => window.frameSizes.length)).toBeGreaterThan(0);
    await page.evaluate(() => window.dispatchEvent(new Event("blur")));
    await expect(page.getByText("Recording", { exact: true })).toHaveCount(0);
    await expect.poll(() => page.evaluate(() => (window as any).captureTracks.every((track: MediaStreamTrack) => track.readyState === "ended"))).toBe(true);
    const count = await page.evaluate(() => window.frameSizes.length);
    await page.waitForTimeout(150);
    expect(await page.evaluate(() => window.frameSizes.length)).toBe(count);
  } finally { server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); }
});
