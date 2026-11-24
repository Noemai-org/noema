import { test as base, expect } from "@playwright/test";
import { readFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { parseInvestigationResponseV1, parseSseEventV1 } from "../build/js/contract_types/customer.js";
import { progressCopy } from "../build/js/progress-copy.js";
import { RESPONSE_FRAME_LIMIT_BYTES } from "../build/js/transport-limits.js";
import { scanVisibleText } from "../build/js/privacy.js";
import { assertAccessible } from "../browser/flow-a11y.mjs";

const manifestPath = resolve(process.cwd(), process.env.NOEMA_UI_E2E_MANIFEST ?? "../benchmark/journeys/j1-j8.v1.json");
const runDir = process.env.NOEMA_UI_E2E_RUN_DIR
  ? resolve(process.cwd(), process.env.NOEMA_UI_E2E_RUN_DIR)
  : null;
const tokenDir = process.env.NOEMA_UI_E2E_TOKEN_DIR
  ? resolve(process.cwd(), process.env.NOEMA_UI_E2E_TOKEN_DIR)
  : null;
const maxPollingMs = 60_000; // A654: UI polling ceiling is 60 seconds.

if (!runDir || !tokenDir) {
  throw new Error("Set NOEMA_UI_E2E_RUN_DIR to the benchmark.runner output and NOEMA_UI_E2E_TOKEN_DIR to the per-principal synthetic token directory");
}

const manifest = JSON.parse(await readFile(manifestPath, "utf8"));
const journeyIds = ["J1", "J2", "J3", "J4", "J5", "J6", "J7", "J8"];
if (!Array.isArray(manifest.journeys) ||
    manifest.journeys.map((journey) => journey.journey).join(",") !== journeyIds.join(",")) {
  throw new Error("The BEN-23 manifest must contain J1 through J8 in PERD order");
}
const cases = manifest.journeys.flatMap((journey) => {
  if (!Array.isArray(journey.cases) || journey.cases.length < 1 || journey.cases.length > 3) {
    throw new Error(`${journey.journey}: BEN-23 must provide one to three generated cases`);
  }
  return journey.cases.map((item) => ({ journey, item }));
});
for (const { journey, item } of cases) {
  if (typeof item.case_id !== "string" || typeof item.principal_id !== "string" ||
      !Array.isArray(item.roles) || item.roles.length !== 1 || !item.roles.includes("customer") ||
      !Array.isArray(item.utterances) || item.utterances.length < 1 ||
      item.utterances.some((utterance) => typeof utterance.text !== "string" || !utterance.text.trim())) {
    throw new Error(`${journey.journey}: malformed BEN-23 case; expected a synthetic customer principal and utterances`);
  }
  if (Object.hasOwn(item, "expected") || Object.hasOwn(item, "oracle_ref") || Object.hasOwn(item, "ground_truth")) {
    throw new Error(`${item.case_id}: BEN-23 is input-only under INV-22; expected results/oracle data must not enter this manifest`);
  }
}

const correlations = JSON.parse(await readFile(join(runDir, "correlations.json"), "utf8"));

async function caseInputs(item) {
  const resultPath = join(runDir, "results", `${item.case_id}.json`);
  const result = JSON.parse(await readFile(resultPath, "utf8"));
  if (result.status !== "COMPLETED") {
    const code = result.error?.code ?? result.status ?? "MISSING_RESULT";
    throw new Error(`${item.case_id}: benchmark.runner did not complete this case (${code})`);
  }
  const correlationId = correlations[item.case_id];
  if (typeof correlationId !== "string" || !/^corr_[0-9A-HJKMNP-TV-Z]{26}$/u.test(correlationId)) {
    throw new Error(`${item.case_id}: benchmark.runner did not record a valid core correlation ID`);
  }
  const token = (await readFile(join(tokenDir, item.principal_id), "utf8")).trim();
  if (!token) throw new Error(`${item.case_id}: no synthetic session token for principal ${item.principal_id}`);
  return { investigationId: `inv_${correlationId.slice(5)}`, token };
}

async function assertVisibleState(page, investigationId, token) {
  const responsePromise = page.waitForResponse((response) =>
    response.request().method() === "GET" &&
    new URL(response.url()).pathname === `/v1/investigations/${investigationId}`
  );
  await page.goto("/");
  await page.evaluate(async ({ id, sessionToken, pollingBound }) => {
    const app = await import("/app.js");
    app.configureOverlay({
      baseUrl: window.location.origin,
      sessionToken,
      maxPollingMs: pollingBound,
      investigationId: id,
    });
  }, { id: investigationId, sessionToken: token, pollingBound: maxPollingMs });

  const response = await responsePromise;
  expect(response.status()).toBe(200);
  const investigation = parseInvestigationResponseV1(await response.json());
  const copy = progressCopy(investigation.state);
  expect(copy, "core must return one of the six public stages").not.toBeNull();
  await expect(page.getByText(copy, { exact: true })).toBeVisible({ timeout: maxPollingMs });
}

async function assertEventsRoute(page, investigationId, token) {
  const frame = await page.evaluate(async ({ path, sessionToken, maxBytes, timeoutMs }) => {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);
    try {
      const response = await fetch(path, {
        method: "GET",
        headers: { Authorization: `Bearer ${sessionToken}` },
        credentials: "omit",
        cache: "no-store",
        redirect: "error",
        signal: controller.signal,
      });
      const headers = {
        status: response.status,
        contentType: response.headers.get("content-type"),
        cacheControl: response.headers.get("cache-control"),
      };
      if (headers.status !== 200 ||
          !/^text\/event-stream(?:;|$)/iu.test(headers.contentType ?? "") ||
          !headers.cacheControl?.includes("no-store") || !response.body) {
        return { headers };
      }

      const reader = response.body.getReader();
      const decoder = new TextDecoder("utf-8", { fatal: true });
      let buffered = "";
      try {
        for (;;) {
          const chunk = await reader.read();
          if (chunk.done) break;
          buffered += decoder.decode(chunk.value, { stream: true });
          for (;;) {
            const boundary = buffered.indexOf("\n\n");
            if (boundary < 0) {
              if (new TextEncoder().encode(buffered).byteLength > maxBytes) {
                throw new Error("Incomplete SSE frame exceeded the approved frame bound");
              }
              break;
            }
            const raw = buffered.slice(0, boundary);
            buffered = buffered.slice(boundary + 2);
            if (new TextEncoder().encode(raw + "\n\n").byteLength > maxBytes) {
              throw new Error("SSE event exceeded the approved frame bound");
            }
            if (raw.startsWith(":")) continue;
            let eventName = "";
            const data = [];
            for (const line of raw.split(/\r?\n/u)) {
              if (line.startsWith("event:")) eventName = line.slice(6).trimStart();
              if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
            }
            if (data.length) return { headers, eventName, payload: JSON.parse(data.join("\n")) };
          }
        }
        return { headers };
      } finally {
        await reader.cancel();
        reader.releaseLock();
      }
    } finally {
      clearTimeout(timer);
      controller.abort();
    }
  }, {
    path: `/v1/conversations/${investigationId}/events`,
    sessionToken: token,
    maxBytes: RESPONSE_FRAME_LIMIT_BYTES,
    timeoutMs: maxPollingMs,
  });

  expect(frame.headers.status).toBe(200);
  expect(frame.headers.contentType).toMatch(/^text\/event-stream(?:;|$)/iu);
  expect(frame.headers.cacheControl).toContain("no-store");
  expect(frame.payload, "the real core stream must deliver a complete contract-backed event").toBeTruthy();
  const event = parseSseEventV1(frame.payload);
  expect(event.event).toBe(frame.eventName);
}

async function assertClean(page) {
  const values = await page.evaluate(() => {
    const root = document.body;
    const found = [root?.textContent ?? ""];
    for (const element of [root, ...root.querySelectorAll("*")]) {
      for (const attribute of element.attributes) found.push(attribute.value);
      if (
        element instanceof HTMLInputElement ||
        element instanceof HTMLTextAreaElement ||
        element instanceof HTMLSelectElement
      ) found.push(element.value);
    }
    return found;
  });
  expect(values.flatMap(scanVisibleText)).toEqual([]);
  await assertAccessible(page);
}

const test = base.extend({
  page: async ({ page }, use) => {
    try {
      page.setDefaultTimeout(60_000);
      page.setDefaultNavigationTimeout(60_000);
      await use(page);
    } finally {
      await assertClean(page);
    }
  },
});

test.describe("REQ-UI-001 · BEN-23 real-core J1–J8 journeys", () => {
  test.describe.configure({ mode: "serial" });

  for (const { journey, item } of cases) {
    test(`${journey.journey} · ${item.case_id}: render benchmark-run core state and events`, async ({ page }) => {
      const { investigationId, token } = await caseInputs(item);
      await assertVisibleState(page, investigationId, token);
      await assertEventsRoute(page, investigationId, token);
    });
  }

  for (const { journey, item } of cases.filter((entry) => entry.journey.journey === "J8")) {
    test(`J8 · ${item.case_id}: resume the same investigation in a fresh browser context`, async ({ browser }) => {
      test.setTimeout(255_000); // two contexts, each with a 60-second state and SSE bound
      const { investigationId, token } = await caseInputs(item);
      const firstContext = await browser.newContext();
      const firstPage = await firstContext.newPage();
      firstPage.setDefaultTimeout(60_000);
      firstPage.setDefaultNavigationTimeout(60_000);
      try {
        await assertVisibleState(firstPage, investigationId, token);
        await assertEventsRoute(firstPage, investigationId, token);
        await assertClean(firstPage);
      } finally {
        await firstContext.close();
      }

      const resumedContext = await browser.newContext();
      const resumedPage = await resumedContext.newPage();
      resumedPage.setDefaultTimeout(60_000);
      resumedPage.setDefaultNavigationTimeout(60_000);
      try {
        await assertVisibleState(resumedPage, investigationId, token);
        await assertEventsRoute(resumedPage, investigationId, token);
        await assertClean(resumedPage);
      } finally {
        await resumedContext.close();
      }
    });
  }
});
