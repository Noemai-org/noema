import test from "node:test";
import assert from "node:assert/strict";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { encodePcm16Mono, PushToTalk, MAX_PUSH_TO_TALK_RECORDING_MS } from "../build/js/push-to-talk.js";
import * as capture from "../build/js/pcm-capture.js";

test("REQ-VOI-001 A191: PCM frames keep whole samples and never exceed 32000 bytes", () => {
  assert.equal(capture.MAX_PCM_FRAME_BYTES, 32000);
  const samples = new Float32Array(16384);
  samples[0] = -1;
  samples[16000] = 1;
  const frames = capture.encodePcm16Frames(samples);
  assert.deepEqual(frames.map((frame) => frame.length), [32000, 768]);
  assert.deepEqual(Array.from(frames[0].slice(0, 2)), [0, 128]);
  assert.deepEqual(Array.from(frames[1].slice(0, 2)), [255, 127]);
  assert.throws(() => capture.encodePcm16Frames(Float32Array.from([0, NaN])), /audio/i);
});

test("REQ-VOI-001 A19: PCM samples are clipped and encoded as signed little-endian 16-bit mono", () => {
  const data = encodePcm16Mono(Float32Array.from([-2, -1, -0.5, 0, 0.5, 1, 2]));
  assert.deepEqual(Array.from(data), [0, 128, 0, 128, 0, 192, 0, 0, 0, 64, 255, 127, 255, 127]);
});

test("REQ-VOI-001: non-finite audio samples are rejected", () => {
  assert.throws(() => encodePcm16Mono(Float32Array.from([0, NaN])), /audio/i);
  assert.throws(() => encodePcm16Mono(Float32Array.from([Infinity])), /audio/i);
});

test("REQ-UI-001: push-to-talk is disabled when no voice sink is supplied", () => {
  const html = renderToStaticMarkup(createElement(PushToTalk));
  assert.match(html, /disabled=""[^>]*>Push to talk/u);
});

test("REQ-VOI-004: a sink without a host recording limit cannot enable capture", () => {
  const html = renderToStaticMarkup(createElement(PushToTalk, { onPcm: () => {} }));
  assert.match(html, /disabled=""[^>]*>Push to talk/u);
});

test("REQ-VOI-004: invalid recording limits are refused before microphone access", async () => {
  for (const value of [undefined, 0, -1, Infinity, 1.5]) {
    await assert.rejects(capture.startPcmCapture(() => {}, new AbortController().signal, undefined, value), /Voice is unavailable/);
  }
});

test("A715: recording limit defaults to 30 seconds and a host cannot extend it", () => {
  assert.equal(MAX_PUSH_TO_TALK_RECORDING_MS, 30_000);
  const defaultHtml = renderToStaticMarkup(createElement(PushToTalk, { onPcm: () => {} }));
  const shorterHtml = renderToStaticMarkup(createElement(PushToTalk, { onPcm: () => {}, maxRecordingMs: 12_000 }));
  const extendedHtml = renderToStaticMarkup(createElement(PushToTalk, { onPcm: () => {}, maxRecordingMs: 90_000 }));
  assert.match(defaultHtml, /<button(?![^>]*disabled)/u);
  assert.match(shorterHtml, /<button(?![^>]*disabled)/u);
  assert.match(extendedHtml, /<button(?![^>]*disabled)/u);
});
