import { test as base, expect } from "@playwright/test";
import { readFile } from "node:fs/promises";
import { startUiFailureServer } from "../test-support/ui-failure-server.mjs";

// This server must serve the real built UI and synthetic HTTP faults; it is not the core.
// Configure the real host bootstrap once the overlay publishes its interface.
const test = base.extend({ page: async ({ page }, use) => {
  try { await use(page); } finally {
    const scanner = await readFile("build/js/privacy.js", "utf8");
    const findings = await page.evaluate(async (source) => {
      const url = URL.createObjectURL(new Blob([source], { type: "text/javascript" }));
      try { return (await import(url)).scanDom(document.body); }
      finally { URL.revokeObjectURL(url); }
    }, scanner);
    expect(findings).toEqual([]);
  }
} });

for (const scenario of ["unavailable", "malformed", "disconnect"]) {
  test(`REQ-UI-001: real UI handles ${scenario} without showing server details`, async ({ page }) => {
    const server = await startUiFailureServer({ scenario });
    try {
      await page.goto(server.origin);
      await page.getByRole("textbox", { name: "Your message" }).fill("Please check this transfer.");
      await page.getByRole("button", { name: "Send" }).click();
      await expect(page.getByRole("status")).toContainText(/connection lost/i);
      await expect(page.locator("body")).not.toContainText("HOSTILE_SERVER_DIAGNOSTIC");
      await expect(page.getByRole("region", { name: "Messages" })).not.toContainText("Synthetic final answer");
    } finally { await server.close(); }
  });
}
