import { defineConfig } from "@playwright/test";
import { existsSync } from "node:fs";

const browser = ["/usr/bin/google-chrome", "/usr/bin/chromium"].find(existsSync);
if (!browser) throw new Error("Browser tests need pre-installed Chrome or Chromium");

export default defineConfig({
  testDir: "browser",
  use: { launchOptions: { executablePath: browser } },
});
