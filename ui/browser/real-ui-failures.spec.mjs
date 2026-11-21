import { test as base, expect } from "@playwright/test";
import { startUiFailureServer } from "../test-support/ui-failure-server.mjs";
import { assertAccessible } from "./flow-a11y.mjs";
import { scanVisibleText } from "../build/js/privacy.js";

// This server must serve the real built UI and synthetic HTTP faults; it is not the core.
// Configure the real host bootstrap once the overlay publishes its interface.
const test = base.extend({ page: async ({ page }, use) => {
  try { await use(page); } finally {
    try {
      const fields = await page.evaluate(() => {
        const root = document.body;
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

for (const scenario of ["unavailable", "malformed", "disconnect", "oversized", "wrong-content-type"]) {
  test(`REQ-UI-001: real UI handles ${scenario} without showing server details`, async ({ page }) => {
    const server = await startUiFailureServer({ scenario });
    try {
      await page.goto(server.origin);
      // Configure the actual browser entry using only the existing investigation contract.
      // The create-conversation route is not yet mounted; Send stays disabled.
      await page.evaluate(async (baseUrl) => {
        const client = await import("/app.js");
        client.configureOverlay({ baseUrl, sessionToken: "synthetic-session", investigationId: "inv_SyntheticA", maxResponseBytes: 65536, maxPollingMs: 15000 });
      }, server.origin);
      // The approved retry sequence alone takes seven seconds.
      await expect(page.getByRole("status")).toContainText(/connection lost/i, { timeout: 20000 });
      await expect(page.locator("body")).not.toContainText("HOSTILE_SERVER_DIAGNOSTIC");
      await expect(page.getByRole("region", { name: "Messages" })).not.toContainText("Synthetic final answer");
    } finally { await server.close(); }
  });
}
