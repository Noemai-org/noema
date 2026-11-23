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
  use: { baseURL: target.origin, launchOptions: { executablePath: browser } },
});
