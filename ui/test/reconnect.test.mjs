import test from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { networkInterfaces } from "node:os";
import { once } from "node:events";
import { readFile } from "node:fs/promises";
import { reconnectOnce } from "../build/js/reconnect.js";

// Local contract server, not the core. All tokens, identifiers and fixtures are SYNTHETIC.
const id = "inv_SyntheticA";
const limit = 16 * 1024; // Synthetic test limit, not a production policy.
const fixture = async (path) => JSON.parse(await readFile(new URL(`../../contracts/fixtures/valid/${path}.json`, import.meta.url), "utf8"));

test("REQ-UI-001: on a real stream drop refetches state before reconnecting, never rendering the partial answer", async () => {
  const address = Object.values(networkInterfaces()).flat().find((entry) => entry?.internal && entry.family === "IPv4")?.address;
  assert.ok(address);
  const state = await fixture("investigation-response/understanding");
  const answer = await fixture("sse-event/answer");
  let firstStream;
  let streamCount = 0;
  let started;
  const firstStarted = new Promise((resolve) => { started = resolve; });
  const trace = [];
  const server = createServer((request, response) => {
    if (request.url === "/drop") {
      assert.ok(firstStream);
      firstStream.destroy();
      response.writeHead(204).end();
      return;
    }
    trace.push({ path: request.url, auth: request.headers.authorization, cookie: request.headers.cookie });
    if (request.url === `/v1/investigations/${id}`) {
      response.writeHead(200, { "content-type": "application/json" });
      response.end(JSON.stringify(state));
    } else {
      streamCount++;
      response.writeHead(200, { "content-type": "text/event-stream" });
      if (streamCount === 1) {
        firstStream = response;
        response.flushHeaders();
        response.write(`event: answer\ndata: ${JSON.stringify(answer).slice(0, 30)}`);
        started();
      } else response.end(`event: answer\ndata: ${JSON.stringify(answer)}\n\n`);
    }
  });
  server.listen(0, address);
  await once(server, "listening");
  try {
    const baseUrl = `http://${address}:${server.address().port}`;
    const events = [];
    const states = [];
    const pending = reconnectOnce({ baseUrl, conversationRef: id, investigationId: id, sessionToken: "synthetic-session", maxFrameBytes: limit, maxResponseBytes: limit, onEvent: (value) => events.push(value), onState: (value) => states.push(value) });
    await firstStarted;
    await fetch(`${baseUrl}/drop`);
    await pending;
    assert.deepEqual(states, [state]);
    assert.deepEqual(events, [answer]);
    assert.deepEqual(trace.map((entry) => entry.path), [`/v1/conversations/${id}/events`, `/v1/investigations/${id}`, `/v1/conversations/${id}/events`]);
    assert.ok(trace.every((entry) => entry.auth === "Bearer synthetic-session" && entry.cookie === undefined));
  } finally {
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
  }
});

test("REQ-UI-001: malformed events and consumer failures do not trigger a retry", async () => {
  const address = Object.values(networkInterfaces()).flat().find((entry) => entry?.internal && entry.family === "IPv4")?.address;
  assert.ok(address);
  const progress = await fixture("sse-event/progress");
  let requests = 0;
  let malformed = true;
  const server = createServer((_request, response) => {
    requests++;
    response.writeHead(200, { "content-type": "text/event-stream" });
    response.end(malformed ? "event: answer\ndata: {}\n\n" : `event: progress\ndata: ${JSON.stringify(progress)}\n\n`);
  });
  server.listen(0, address);
  await once(server, "listening");
  try {
    const baseUrl = `http://${address}:${server.address().port}`;
    const input = { baseUrl, conversationRef: id, investigationId: id, sessionToken: "synthetic-session", maxFrameBytes: limit, maxResponseBytes: limit, onEvent: () => {}, onState: () => assert.fail("unexpected refetch") };
    await assert.rejects(reconnectOnce(input));
    assert.equal(requests, 1);
    malformed = false;
    await assert.rejects(reconnectOnce({ ...input, onEvent: () => { throw new TypeError("consumer failure"); } }), /consumer failure/);
    assert.equal(requests, 2);
  } finally {
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
  }
});

test("REQ-UI-001: clean EOF in a partial frame also refetches before reconnecting", async () => {
  const address = Object.values(networkInterfaces()).flat().find((entry) => entry?.internal && entry.family === "IPv4")?.address;
  assert.ok(address);
  const state = await fixture("investigation-response/understanding");
  const progress = await fixture("sse-event/progress");
  const paths = [];
  let streams = 0;
  const server = createServer((request, response) => {
    paths.push(request.url);
    if (request.url === `/v1/investigations/${id}`) {
      response.writeHead(200, { "content-type": "application/json" }).end(JSON.stringify(state));
    } else {
      streams++;
      response.writeHead(200, { "content-type": "text/event-stream" });
      response.end(streams === 1 ? "event: answer\ndata: {" : `event: progress\ndata: ${JSON.stringify(progress)}\n\n`);
    }
  });
  server.listen(0, address);
  await once(server, "listening");
  try {
    const events = [];
    const states = [];
    await reconnectOnce({ baseUrl: `http://${address}:${server.address().port}`, conversationRef: id, investigationId: id, sessionToken: "synthetic-session", maxFrameBytes: limit, maxResponseBytes: limit, onEvent: (value) => events.push(value), onState: (value) => states.push(value) });
    assert.deepEqual(states, [state]);
    assert.deepEqual(events, [progress]);
    assert.deepEqual(paths, [`/v1/conversations/${id}/events`, `/v1/investigations/${id}`, `/v1/conversations/${id}/events`]);
  } finally {
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
  }
});

test("REQ-UI-001: a completed answer does not trigger another request", async () => {
  const address = Object.values(networkInterfaces()).flat().find((entry) => entry?.internal && entry.family === "IPv4")?.address;
  assert.ok(address);
  const answer = await fixture("sse-event/answer");
  let requests = 0;
  const server = createServer((_request, response) => {
    requests++;
    response.writeHead(200, { "content-type": "text/event-stream" });
    response.end(`event: answer\ndata: ${JSON.stringify(answer)}\n\n`);
  });
  server.listen(0, address);
  await once(server, "listening");
  try {
    const events = [];
    await reconnectOnce({ baseUrl: `http://${address}:${server.address().port}`, conversationRef: id, investigationId: id, sessionToken: "synthetic-session", maxFrameBytes: limit, maxResponseBytes: limit, onEvent: (value) => events.push(value), onState: () => assert.fail("unexpected refetch") });
    assert.deepEqual(events, [answer]);
    assert.equal(requests, 1);
  } finally {
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
  }
});

test("REQ-UI-001: refetch with a final answer does not reopen the stream", async () => {
  const address = Object.values(networkInterfaces()).flat().find((entry) => entry?.internal && entry.family === "IPv4")?.address;
  assert.ok(address);
  const state = await fixture("investigation-response/completed_with_answer");
  let firstStream;
  let started;
  const firstStarted = new Promise((resolve) => { started = resolve; });
  const paths = [];
  const server = createServer((request, response) => {
    if (request.url === "/drop") {
      firstStream.destroy();
      response.writeHead(204).end();
      return;
    }
    paths.push(request.url);
    if (request.url === `/v1/investigations/${id}`) {
      response.writeHead(200, { "content-type": "application/json" }).end(JSON.stringify(state));
    } else if (paths.length === 1) {
      firstStream = response;
      response.writeHead(200, { "content-type": "text/event-stream" });
      response.flushHeaders();
      started();
    } else response.writeHead(503).end();
  });
  server.listen(0, address);
  await once(server, "listening");
  try {
    const states = [];
    const baseUrl = `http://${address}:${server.address().port}`;
    const pending = reconnectOnce({ baseUrl, conversationRef: id, investigationId: id, sessionToken: "synthetic-session", maxFrameBytes: limit, maxResponseBytes: limit, onEvent: () => assert.fail("unexpected event"), onState: (value) => states.push(value) });
    await firstStarted;
    await fetch(`${baseUrl}/drop`);
    await pending;
    assert.deepEqual(states, [state]);
    assert.deepEqual(paths, [`/v1/conversations/${id}/events`, `/v1/investigations/${id}`]);
  } finally {
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
  }
});


test("A715: an oversized SSE frame falls back to authoritative polling without reopening SSE", async () => {
  const address = Object.values(networkInterfaces()).flat().find((entry) => entry?.internal && entry.family === "IPv4")?.address;
  assert.ok(address);
  const terminal = await fixture("investigation-response/completed_with_answer");
  const paths = [];
  const server = createServer((request, response) => {
    paths.push(request.url);
    if (request.url === `/v1/conversations/${id}/events`) {
      response.writeHead(200, { "content-type": "text/event-stream" });
      response.end(":" + "x".repeat(65_537) + "\n\n");
      return;
    }
    if (request.url === `/v1/investigations/${id}`) {
      response.writeHead(200, { "content-type": "application/json" }).end(JSON.stringify(terminal));
      return;
    }
    response.writeHead(404).end();
  });
  server.listen(0, address);
  await once(server, "listening");
  try {
    const states = [];
    await reconnectOnce({
      baseUrl: `http://${address}:${server.address().port}`,
      conversationRef: id,
      investigationId: id,
      sessionToken: "synthetic-session",
      maxFrameBytes: 65_536,
      maxResponseBytes: 65_536,
      onEvent: () => assert.fail("oversized frame must never be delivered"),
      onState: (value) => states.push(value),
    });
    assert.deepEqual(states, [terminal]);
    assert.deepEqual(paths, [`/v1/conversations/${id}/events`, `/v1/investigations/${id}`]);
  } finally {
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
  }
});
