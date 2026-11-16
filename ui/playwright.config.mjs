import { defineConfig } from "@playwright/test";
import { existsSync } from "node:fs";

const executablePath = ["/usr/bin/google-chrome", "/usr/bin/chromium"].find(existsSync);
if (!executablePath) throw new Error("Browser tests require pre-installed Chrome or Chromium");
export default defineConfig({ testDir: "browser", use: { launchOptions: { executablePath } } });
