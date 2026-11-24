import { defineConfig } from "@playwright/test";
import { existsSync } from "node:fs";

// A388: explicit opt-in by IL's stack job; never substitute a local test server.
const url = process.env.NOEMA_UI_URL;
if (!url) throw new Error("NOEMA_UI_URL is required for stack browser checks");
const target = new URL(url);
if (target.protocol !== "http:" || !["localhost", "127.0.0.1", "[::1]"].includes(target.hostname) || target.username || target.password) {
  throw new Error("Stack browser checks require the configured loopback UI URL");
}
const browser = ["/usr/bin/google-chrome", "/usr/bin/chromium"].find(existsSync);
if (!browser) throw new Error("Stack browser checks need pre-installed Chrome or Chromium");

export default defineConfig({
  testDir: "browser-stack",
  timeout: 135_000, // one 60-second state wait + one 60-second SSE read, with cleanup margin
  expect: { timeout: 60_000 },
  use: {
    baseURL: target.origin,
    actionTimeout: 60_000,
    navigationTimeout: 60_000,
    launchOptions: { executablePath: browser },
  },
});
