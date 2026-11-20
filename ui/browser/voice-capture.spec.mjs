import { test as base, expect } from "@playwright/test";
import { createServer } from "node:http";
import { once } from "node:events";
import { networkInterfaces } from "node:os";
import { existsSync, readFileSync } from "node:fs";

const browser = ["/usr/bin/google-chrome", "/usr/bin/chromium"].find(existsSync);
const test = base.extend({
  page: async ({ page }, use) => {
    try { await use(page); } finally {
      const source = readFileSync("build/js/privacy.js", "utf8");
      const findings = await page.evaluate(async (sourceText) => {
        const url = URL.createObjectURL(new Blob([sourceText], { type: "text/javascript" }));
        try { return (await import(url)).scanDom(document.body); }
        finally { URL.revokeObjectURL(url); }
      }, source);
      expect(findings).toEqual([]);
    }
  },
});
// The device is Chromium's deterministic synthetic audio input, not a recorded person.
test.use({ launchOptions: { executablePath: browser, args: ["--use-fake-device-for-media-stream"] } });

let server;
let origin;
test.beforeAll(async () => {
  const address = Object.values(networkInterfaces()).flat().find((item) => item?.internal && item.family === "IPv4")?.address;
  if (!address) throw new Error("No local IPv4 interface for browser test");
  server = createServer((_request, response) => response.writeHead(200, { "content-type": "text/html" }).end("<!doctype html><body>Voice capture test</body>"));
  server.listen(0, address);
  await once(server, "listening");
  origin = `http://${address}:${server.address().port}`;
});
test.afterAll(async () => {
  server?.closeAllConnections();
  if (server) await new Promise((resolve) => server.close(resolve));
});

test("REQ-VOI-001: a real browser captures mono PCM and stops its microphone on release", async ({ page, context }) => {
  await context.grantPermissions(["microphone"], { origin });
  await page.goto(origin);
  const source = readFileSync("build/js/pcm-capture.js", "utf8");
  await page.evaluate(async (sourceText) => {
    const NativeAudioContext = window.AudioContext;
    window.AudioContext = class extends NativeAudioContext {
      constructor(options) { super(options); globalThis.captureSampleRate = this.sampleRate; }
      createScriptProcessor(_size, inputChannels, outputChannels) {
        return super.createScriptProcessor(16384, inputChannels, outputChannels);
      }
    };
    const original = navigator.mediaDevices.getUserMedia.bind(navigator.mediaDevices);
    navigator.mediaDevices.getUserMedia = async (constraints) => {
      const stream = await original(constraints);
      globalThis.captureTracks = stream.getTracks();
      return stream;
    };
    const url = URL.createObjectURL(new Blob([sourceText], { type: "text/javascript" }));
    try {
      const { startPcmCapture } = await import(url);
      globalThis.audioChunks = [];
      globalThis.abortCapture = new AbortController();
      globalThis.stopCapture = await startPcmCapture((bytes) => globalThis.audioChunks.push(bytes.length), globalThis.abortCapture.signal);
    } finally { URL.revokeObjectURL(url); }
  }, source);
  await expect.poll(() => page.evaluate(() => globalThis.audioChunks.length)).toBeGreaterThan(0);
  const result = await page.evaluate(async () => {
    globalThis.abortCapture.abort();
    await globalThis.stopCapture();
    return { rate: globalThis.captureSampleRate, chunks: globalThis.audioChunks, tracks: globalThis.captureTracks.map((track) => track.readyState) };
  });
  expect(result.rate).toBe(16000);
  expect(result.chunks.every((size) => size > 0 && size % 2 === 0)).toBe(true);
  expect(result.chunks).toContain(32000);
  expect(result.chunks).toContain(768);
  expect(result.chunks.every((size) => size <= 32000)).toBe(true);
  expect(result.tracks.length).toBeGreaterThan(0);
  expect(result.tracks.every((state) => state === "ended")).toBe(true);
});

test("REQ-VOI-001: denying microphone permission fails closed", async ({ page, context }) => {
  await context.grantPermissions([], { origin });
  await page.goto(origin);
  const source = readFileSync("build/js/pcm-capture.js", "utf8");
  const result = await page.evaluate(async (sourceText) => {
    const url = URL.createObjectURL(new Blob([sourceText], { type: "text/javascript" }));
    try {
      const { startPcmCapture } = await import(url);
      try { await startPcmCapture(() => {}, new AbortController().signal); return "unexpected success"; }
      catch { return "denied"; }
    } finally { URL.revokeObjectURL(url); }
  }, source);
  expect(result).toBe("denied");
});

test("REQ-VOI-001: an ended microphone track stops capture and reports failure", async ({ page, context }) => {
  await context.grantPermissions(["microphone"], { origin });
  await page.goto(origin);
  const source = readFileSync("build/js/pcm-capture.js", "utf8");
  const result = await page.evaluate(async (sourceText) => {
    const original = navigator.mediaDevices.getUserMedia.bind(navigator.mediaDevices);
    let track;
    navigator.mediaDevices.getUserMedia = async (constraints) => {
      const stream = await original(constraints);
      track = stream.getAudioTracks()[0];
      return stream;
    };
    const url = URL.createObjectURL(new Blob([sourceText], { type: "text/javascript" }));
    try {
      const { startPcmCapture } = await import(url);
      let resolveFailure;
      const failed = new Promise((resolve) => { resolveFailure = resolve; });
      await startPcmCapture(() => {}, new AbortController().signal, resolveFailure);
      // Simulate the device's ended notification on a real synthetic microphone track.
      track.dispatchEvent(new Event("ended"));
      await failed;
      return track.readyState;
    } finally { URL.revokeObjectURL(url); }
  }, source);
  expect(result).toBe("ended");
});
