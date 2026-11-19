import { test as base, expect } from "@playwright/test";
import { readFile } from "node:fs/promises";

// Real-core boundary check, opt-in for a dedicated synthetic tenant and correlation.
// This is not a panel interaction test and does not establish UI-03 acceptance.
const enabled = process.env.NOEMA_RECONSTRUCTION_ACCESS_TEST === "1";
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
test.skip(!enabled, "Requires an explicitly configured synthetic real-core test tenant");

test("REQ-AUD-003: reconstruction is restricted to ops and audit sessions", async ({ page }) => {
  const origin = process.env.NOEMA_RECONSTRUCTION_BASE_URL;
  const correlation = process.env.NOEMA_RECONSTRUCTION_CORRELATION_ID;
  const ops = process.env.NOEMA_RECONSTRUCTION_OPS_TOKEN;
  const audit = process.env.NOEMA_RECONSTRUCTION_AUDIT_TOKEN;
  const customer = process.env.NOEMA_RECONSTRUCTION_CUSTOMER_TOKEN;
  const approver = process.env.NOEMA_RECONSTRUCTION_APPROVER_TOKEN;
  expect(Boolean(origin && correlation && ops && audit && customer && approver)).toBe(true);
  expect(new Set([ops, audit, customer, approver]).size).toBe(4);
  const baseAddress = new URL(origin);
  const local = baseAddress.protocol === "http:" && (baseAddress.hostname === "localhost" || baseAddress.hostname === "[::1]" || /^127(?:\.\d{1,3}){3}$/u.test(baseAddress.hostname));
  expect((baseAddress.protocol === "https:" || local) && !baseAddress.username && !baseAddress.password).toBe(true);
  expect(correlation).toMatch(/^corr_[0-9A-Za-z]{2,32}$/u);
  const url = new URL(`/v1/reconstructions/${encodeURIComponent(correlation)}`, baseAddress).toString();
  const get = (token) => page.request.get(url, { headers: token ? { authorization: `Bearer ${token}` } : {}, maxRedirects: 0 });
  for (const token of [ops, audit]) expect((await get(token)).status()).toBe(200);
  for (const token of [customer, approver]) expect((await get(token)).status()).toBe(404);
  expect((await get(undefined)).status()).toBe(401);
});
