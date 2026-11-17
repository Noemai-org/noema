import test from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { networkInterfaces } from "node:os";
import { once } from "node:events";
import { readFile } from "node:fs/promises";
import { readEventStream } from "../build/js/sse-reader.js";

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
    await readEventStream(response, (event) => events.push(event));
    assert.deepEqual(events, [progress, answer]);
  });
});

test("REQ-UI-001: a real dropped connection cannot deliver an incomplete answer", async () => {
  const answer = await fixture("answer");
  await withServer((_request, response) => {
    response.writeHead(200, { "content-type": "text/event-stream" });
    response.flushHeaders();
    response.write(`event: answer\ndata: ${JSON.stringify(answer).slice(0, 30)}`);
    setImmediate(() => response.destroy());
  }, async (url) => {
    const response = await fetch(url);
    const events = [];
    await assert.rejects(readEventStream(response, (event) => events.push(event)));
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
    await assert.rejects(readEventStream(response, (event) => events.push(event)));
    assert.deepEqual(events, []);
  });
});
