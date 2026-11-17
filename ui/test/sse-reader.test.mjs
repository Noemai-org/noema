import test from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { networkInterfaces } from "node:os";
import { once } from "node:events";
import { readFile } from "node:fs/promises";
import { readEventStream } from "../build/js/sse-reader.js";

// SYNTHETIC test limit, supplied by the caller; this is not a production policy.
const maxFrameBytes = 16 * 1024;

// Local contract server, not the core. Synthetic fixtures only; faults are real socket faults.
async function withServer(handler, run) {
  const address = Object.values(networkInterfaces()).flat().find((item) => item?.internal && item.family === "IPv4")?.address;
  assert.ok(address, "A loopback IPv4 interface is required");
  const server = createServer(handler);
  server.listen(0, address);
  await once(server, "listening");
  try { await run(`http://${address}:${server.address().port}`); }
  finally { server.closeAllConnections(); await new Promise((resolve) => server.close(resolve)); }
}

async function fixture(name) {
  return JSON.parse(await readFile(new URL(`../../contracts/fixtures/valid/sse-event/${name}.json`, import.meta.url), "utf8"));
}

test("REQ-UI-001: only complete validated SSE events are delivered", async () => {
  const progress = await fixture("progress");
  const answer = await fixture("answer");
  await withServer((_request, response) => {
    response.writeHead(200, { "content-type": "text/event-stream" });
    response.write(`event: progress\ndata: ${JSON.stringify(progress)}\n\n`);
    response.end(`event: answer\ndata: ${JSON.stringify(answer)}\n\n`);
  }, async (url) => {
    const response = await fetch(url);
    const events = [];
    await readEventStream(response, (event) => events.push(event), maxFrameBytes);
    assert.deepEqual(events, [progress, answer]);
  });
});

test("REQ-UI-001: a real dropped connection cannot deliver an incomplete answer", async () => {
  const answer = await fixture("answer");
  let stream;
  await withServer((request, response) => {
    if (request.url === "/drop") {
      assert.ok(stream);
      stream.destroy();
      response.writeHead(204).end();
      return;
    }
    stream = response;
    response.writeHead(200, { "content-type": "text/event-stream" });
    response.flushHeaders();
    response.write(`event: answer\ndata: ${JSON.stringify(answer).slice(0, 30)}`);
  }, async (url) => {
    const response = await fetch(url);
    const events = [];
    const pending = readEventStream(response, (event) => events.push(event), maxFrameBytes);
    await fetch(`${url}/drop`);
    await assert.rejects(pending);
    assert.deepEqual(events, []);
  });
});

test("REQ-UI-001: event names must agree with the contract payload", async () => {
  const progress = await fixture("progress");
  await withServer((_request, response) => {
    response.writeHead(200, { "content-type": "text/event-stream" });
    response.end(`event: answer\ndata: ${JSON.stringify(progress)}\n\n`);
  }, async (url) => {
    const response = await fetch(url);
    const events = [];
    await assert.rejects(readEventStream(response, (event) => events.push(event), maxFrameBytes));
    assert.deepEqual(events, []);
  });
});

test("REQ-UI-001: CRLF frames accept a split UTF-8 event without inventing content", async () => {
  const event = await fixture("answer");
  event.answer.found[0] += " é";
  const frame = Buffer.from(`event: answer\r\ndata: ${JSON.stringify(event)}\r\n\r\n`);
  const nonAscii = frame.findIndex((byte) => byte > 127);
  assert.ok(nonAscii > 0);
  await withServer((_request, response) => {
    response.writeHead(200, { "content-type": "text/event-stream; charset=utf-8" });
    response.write(frame.subarray(0, nonAscii + 1));
    response.end(frame.subarray(nonAscii + 1));
  }, async (url) => {
    const events = [];
    await readEventStream(await fetch(url), (item) => events.push(item), maxFrameBytes);
    assert.deepEqual(events, [event]);
  });
});

test("REQ-UI-001: oversized frames and real HTTP errors do not deliver server text", async () => {
  await withServer((request, response) => {
    if (request.url === "/error") {
      response.writeHead(503, { "content-type": "application/json" });
      response.end('{"message":"synthetic private text"}');
    } else {
      response.writeHead(200, { "content-type": "text/event-stream" });
      response.end(`event: answer\ndata: ${"x".repeat(maxFrameBytes + 1)}\n\n`);
    }
  }, async (url) => {
    for (const suffix of ["/error", "/oversized"]) {
      const events = [];
      const response = await fetch(`${url}${suffix}`);
      await assert.rejects(readEventStream(response, (event) => events.push(event), maxFrameBytes), (error) => {
        assert.ok(!String(error).includes("synthetic private text"));
        return true;
      });
      assert.deepEqual(events, []);
    }
  });
});

test("REQ-UI-001: wrong content type and malformed or truncated frames fail closed", async () => {
  await withServer((request, response) => {
    if (request.url === "/html") {
      response.writeHead(200, { "content-type": "text/html" });
      response.end("<p>synthetic server error</p>");
    } else {
      response.writeHead(200, { "content-type": "text/event-stream" });
      response.end(request.url === "/malformed" ? "event: answer\ndata: {bad}\n\n" : "event: answer\ndata: {");
    }
  }, async (url) => {
    for (const suffix of ["/html", "/malformed", "/truncated"]) {
      const events = [];
      const response = await fetch(`${url}${suffix}`);
      await assert.rejects(readEventStream(response, (event) => events.push(event), maxFrameBytes));
      assert.deepEqual(events, []);
    }
  });
});
