import { test as base, expect } from "@playwright/test";
import { readFile } from "node:fs/promises";
import { parseConversationResponseV1, parseSseEventV1 } from "../build/js/contract_types/customer.js";
import { RESPONSE_FRAME_LIMIT_BYTES } from "../build/js/transport-limits.js";
import { scanVisibleText } from "../build/js/privacy.js";
import { assertAccessible } from "../browser/flow-a11y.mjs";

const token = process.env.NOEMA_UI_E2E_SESSION_TOKEN;
const manifestPath = process.env.NOEMA_UI_E2E_MANIFEST;
const maxPollingMs = 60_000; // A654: UI polling ceiling is 60 seconds.

if (!token || !manifestPath || !Number.isSafeInteger(maxPollingMs) ||
    maxPollingMs <= 0 || maxPollingMs > 2_147_483_647) {
  throw new Error(
    "Supply a short-lived synthetic customer token and J1–J8 manifest",
  );
}

const manifest = JSON.parse(await readFile(manifestPath, "utf8"));
const journeyIds = ["J1", "J2", "J3", "J4", "J5", "J6", "J7", "J8"];
const stageCopy = {
  understanding: "We're reading your request.",
  needs_input: "We need more information from you.",
  needs_confirmation: "Please review the action.",
  working: "We're checking the details.",
  answered: "This request has ended.",
  unavailable: "We can't continue right now.",
};

if (
  manifest?.schema_version !== "noema-ui-journeys.v1" ||
  !manifest.journeys ||
  journeyIds.some((id) => {
    const journey = manifest.journeys[id];
    return !journey ||
      typeof journey.message !== "string" ||
      !journey.message.trim() ||
      !Object.hasOwn(stageCopy, journey.expected_stage) ||
      typeof journey.expected_text !== "string" ||
      !journey.expected_text.trim();
  })
) {
  throw new Error(
    "Manifest must define message, expected_stage and expected_text for pre-seeded synthetic J1–J8",
  );
}

const test = base.extend({
  page: async ({ page }, use) => {
    try {
      await use(page);
    } finally {
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
  },
});

async function setup(page, investigationId) {
  await page.goto("/");
  await expect(page.getByRole("textbox", { name: "Your message" })).toBeVisible();
  await page.evaluate(async ({ id, sessionToken, pollingBound }) => {
    const app = await import("/app.js");
    app.configureOverlay({
      baseUrl: window.location.origin,
      sessionToken,
      maxPollingMs: pollingBound,
      ...(id ? { investigationId: id } : {}),
    });
  }, { id: investigationId, sessionToken: token, pollingBound: maxPollingMs });
}

// Probe the core-issued stream URL directly. The shipped UI currently consumes the
// investigation GET/polling path; this independently verifies the events route itself.
async function assertEventsRoute(page, streamUrl) {
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
      if (headers.status !== 200 || !/^text\\/event-stream(?:;|$)/iu.test(headers.contentType ?? "") ||
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
            const boundary = buffered.indexOf("\\n\\n");
            if (boundary < 0) break;
            const raw = buffered.slice(0, boundary);
            buffered = buffered.slice(boundary + 2);
            if (new TextEncoder().encode(raw + "\\n\\n").byteLength > maxBytes) {
              throw new Error("SSE event exceeded the approved frame bound");
            }
            if (raw.startsWith(":")) continue;
            let eventName = "";
            const data = [];
            for (const line of raw.split(/\\r?\\n/u)) {
              if (line.startsWith("event:")) eventName = line.slice(6).trimStart();
              if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
            }
            if (data.length) return { headers, eventName, payload: JSON.parse(data.join("\\n")) };
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
  }, { path: streamUrl, sessionToken: token, maxBytes: RESPONSE_FRAME_LIMIT_BYTES, timeoutMs: maxPollingMs });

  expect(frame.headers.status).toBe(200);
  expect(frame.headers.contentType).toMatch(/^text\\/event-stream(?:;|$)/iu);
  expect(frame.headers.cacheControl).toContain("no-store");
  expect(frame.payload, "the real core stream must deliver at least one complete contract-backed event").toBeTruthy();
  const event = parseSseEventV1(frame.payload);
  expect(event.event).toBe(frame.eventName);
}

async function submit(page, journey) {
  const postResponse = page.waitForResponse((response) =>
    response.request().method() === "POST" &&
    new URL(response.url()).pathname === "/v1/conversations"
  );

  await page.getByRole("textbox", { name: "Your message" }).fill(journey.message);
  await page.getByRole("button", { name: "Send" }).click();

  const response = await postResponse;
  expect([200, 202]).toContain(response.status());
  expect(response.headers()["content-type"]).toMatch(/^application\/json(?:;|$)/iu);

  const conversation = parseConversationResponseV1(await response.json());
  expect(conversation.stream_url).toBe(
    `/v1/conversations/${conversation.investigation_id}/events`,
  );
  expect(conversation.conversation_ref).toBe(conversation.investigation_id);
  await assertEventsRoute(page, conversation.stream_url);

  return conversation.investigation_id;
}

test.describe("REQ-UI-001 · real-core J1–J8", () => {
  test.describe.configure({ mode: "serial" });

  for (const id of journeyIds.filter((journeyId) => journeyId !== "J8")) {
    test(`${id}: core public stage and customer text`, async ({ page }) => {
      const journey = manifest.journeys[id];
      await setup(page);
      const investigationId = await submit(page, journey);

      await expect(
        page.getByText(stageCopy[journey.expected_stage], { exact: true }),
      ).toBeVisible({ timeout: maxPollingMs });
      await expect(page.locator("body")).toContainText(
        journey.expected_text,
        { timeout: maxPollingMs },
      );
      expect(investigationId).toMatch(/^inv_[0-9A-HJKMNP-TV-Z]{26}$/u);
    });
  }

  test("J8: a fresh browser context resumes the same investigation", async ({ browser }) => {
    const journey = manifest.journeys.J8;
    const firstContext = await browser.newContext();
    const firstPage = await firstContext.newPage();
    let investigationId;

    try {
      await setup(firstPage);
      investigationId = await submit(firstPage, journey);
      await expect(
        firstPage.getByText(stageCopy[journey.expected_stage], { exact: true }),
      ).toBeVisible({ timeout: maxPollingMs });
      await expect(firstPage.locator("body")).toContainText(
        journey.expected_text,
        { timeout: maxPollingMs },
      );
    } finally {
      await firstContext.close();
    }

    const resumedContext = await browser.newContext();
    const resumedPage = await resumedContext.newPage();

    try {
      await setup(resumedPage, investigationId);
      await assertEventsRoute(
        resumedPage,
        `/v1/conversations/${investigationId}/events`,
      );
      await expect(
        resumedPage.getByText(stageCopy[journey.expected_stage], { exact: true }),
      ).toBeVisible({ timeout: maxPollingMs });
      await expect(resumedPage.locator("body")).toContainText(
        journey.expected_text,
        { timeout: maxPollingMs },
      );
    } finally {
      await resumedContext.close();
    }
  });
});
