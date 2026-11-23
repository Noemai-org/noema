import { test as base, expect } from "@playwright/test";
import { readFile } from "node:fs/promises";
import { parseInvestigationResponseV1 } from "../build/js/contract_types/customer.js";
import { isSecureInvestigationTarget } from "../build/js/investigation-client.js";

// Opt-in only, for a dedicated synthetic test investigation on the real route.
// These are route checks; they do not claim browser UI or real Android US-01 coverage.
const enabled = process.env.NOEMA_REAL_ROUTE_TEST === "1";
const baseUrl = process.env.NOEMA_REAL_ROUTE_BASE_URL;
const investigationId = process.env.NOEMA_REAL_INVESTIGATION_ID;
const ownerToken = process.env.NOEMA_REAL_OWNER_TOKEN;
const otherToken = process.env.NOEMA_REAL_OTHER_TOKEN;
const scanner = await readFile("build/js/privacy.js", "utf8");
const test = base.extend({ page: async ({ page }, use) => {
  try { await use(page); } finally {
    const findings = await page.evaluate(async (source) => {
      const url = URL.createObjectURL(new Blob([source], { type: "text/javascript" }));
      try { return (await import(url)).scanDom(document.body); }
      finally { URL.revokeObjectURL(url); }
    }, scanner);
    expect(findings).toEqual([]);
  }
} });
test.skip(!enabled, "Requires a dedicated synthetic real-core investigation and explicit opt-in");

test("real investigation route enforces ownership and returns a public contract", async ({ page }) => {
  expect(baseUrl && investigationId && ownerToken && otherToken).toBeTruthy();
  expect(ownerToken).not.toBe(otherToken);
  const baseAddress = new URL(baseUrl);
  expect(isSecureInvestigationTarget(baseAddress)).toBe(true);
  expect(investigationId).toMatch(/^inv_[0-9A-Za-z]{2,32}$/u);
  const url = new URL(`/v1/investigations/${encodeURIComponent(investigationId)}`, baseAddress).toString();
  const get = (token) => page.request.get(url, {
    headers: token ? { authorization: `Bearer ${token}` } : {},
    maxRedirects: 0,
  });
  const own = await get(ownerToken);
  expect(own.status()).toBe(200);
  expect(own.headers()["cache-control"]).toContain("no-store");
  expect(own.headers()["content-type"]).toMatch(/^application\/json(?:;|$)/iu);
  const body = await own.json();
  expect(() => parseInvestigationResponseV1(body)).not.toThrow();
  const foreign = await get(otherToken);
  expect(foreign.status()).toBe(404);
  expect((await get(undefined)).status()).toBe(401);
});
