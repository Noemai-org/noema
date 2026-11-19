import test from "node:test";
import assert from "node:assert/strict";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { encodePcm16Mono, PushToTalk } from "../build/js/push-to-talk.js";

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
