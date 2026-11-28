import test from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { networkInterfaces } from "node:os";
import { readFile } from "node:fs/promises";
import { once } from "node:events";
import { isSecureTarget, submitClarification, submitConfirmation } from "../build/js/card-submit.js";
import { parseMessageRequestV1 } from "../build/js/contract_types/customer.js";

// Local contract server, not the core. All identifiers, tokens and clock values are SYNTHETIC.
const bearer = "synthetic-session";
const conversation = "inv_SyntheticA";
const cardRequestId = "7c1d4e52-9b3a-4f60-8e17-2a5c6d0b9f34";

test("REQ-UI-002: bearer tokens are never sent to a non-loopback HTTP origin", () => {
  assert.equal(isSecureTarget(new URL("http://example.invalid")), false);
  assert.equal(isSecureTarget(new URL("http://127.0.0.1.example.invalid")), false);
  assert.equal(isSecureTarget(new URL("https://user@example.invalid")), false);
  assert.equal(isSecureTarget(new URL("https://example.invalid")), true);
  assert.equal(isSecureTarget(new URL("http://localhost")), true);
});
const currentCard = async (name) => ({
  ...JSON.parse(await readFile(new URL(`../../contracts/fixtures/valid/overlay-card/${name}.json`, import.meta.url), "utf8")),
  expires_at: "2099-01-01T00:00:00Z",
});

async function withServer(handler, run) {
  const address = Object.values(networkInterfaces()).flat().find((entry) => entry?.internal && entry.family === "IPv4")?.address;
  if (!address) throw new Error("No local IPv4 interface for the contract server");
  const server = createServer(handler);
  server.listen(0, address);
  await once(server, "listening");
  try {
    const listener = server.address();
    assert.ok(listener && typeof listener !== "string");
    await run(`http://${address}:${listener.port}`);
  } finally {
    server.closeAllConnections();
    await new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
  }
}

async function bodyOf(request) {
  const chunks = [];
  for await (const chunk of request) chunks.push(chunk);
  return JSON.parse(Buffer.concat(chunks).toString("utf8"));
}

test("REQ-UI-002 US-02: a clarification submits the bound card and selected server option", async () => {
  const card = await currentCard("clarification_four_options");
  let seen;
  await withServer(async (request, response) => {
    seen = { path: request.url, method: request.method, auth: request.headers.authorization, cookie: request.headers.cookie, body: await bodyOf(request) };
    response.writeHead(202, { "content-type": "application/json" });
    response.end(JSON.stringify({ state: "working" }));
  }, async (baseUrl) => {
    const result = await submitClarification({ baseUrl, conversationRef: conversation, sessionToken: bearer, card, optionId: card.options[0].option_id, requestId: cardRequestId });
    assert.equal(result, "accepted");
  });
  assert.equal(seen.path, `/v1/conversations/${conversation}/messages`);
  assert.equal(seen.method, "POST");
  assert.equal(seen.auth, `Bearer ${bearer}`);
  assert.equal(seen.cookie, undefined);
  assert.deepEqual(Object.keys(seen.body).sort(), ["clarification_id", "option_id", "request_id"]);
  assert.equal(seen.body.clarification_id, card.clarification_id);
  assert.equal(seen.body.option_id, card.options[0].option_id);
  assert.match(seen.body.request_id, /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/);
  assert.doesNotThrow(() => parseMessageRequestV1(seen.body));
});

test("REQ-UI-002 US-18: a confirmation submits the bound server ID and an allowed response", async () => {
  const card = await currentCard("confirmation");
  let seen;
  await withServer(async (request, response) => {
    seen = { path: request.url, auth: request.headers.authorization, body: await bodyOf(request) };
    response.writeHead(202, { "content-type": "application/json" });
    response.end(JSON.stringify({ state: "working" }));
  }, async (baseUrl) => {
    const result = await submitConfirmation({ baseUrl, sessionToken: bearer, card, response: "DECLINE" });
    assert.equal(result, "accepted");
  });
  assert.equal(seen.path, `/v1/confirmations/${card.confirmation_request_id}`);
  assert.equal(seen.auth, `Bearer ${bearer}`);
  assert.deepEqual(Object.keys(seen.body).sort(), ["request_id", "response"]);
  // confirmation-answer.v1 binds this to the request being answered, not a new UUID.
  assert.equal(seen.body.request_id, card.confirmation_request_id);
  assert.equal(seen.body.response, "DECLINE");
});

test("A491: clarification retries reuse the card's request ID", async () => {
  const card = await currentCard("clarification_four_options");
  const requestId = crypto.randomUUID();
  const bodies = [];
  await withServer(async (request, response) => {
    bodies.push(await bodyOf(request));
    response.writeHead(202).end();
  }, async (baseUrl) => {
    const input = { baseUrl, conversationRef: conversation, sessionToken: bearer, card, optionId: card.options[0].option_id, requestId };
    assert.equal(await submitClarification(input), "accepted");
    assert.equal(await submitClarification(input), "accepted");
  });
  assert.equal(bodies.length, 2);
  assert.equal(bodies[0].request_id, requestId);
  assert.equal(bodies[1].request_id, requestId);
  assert.doesNotThrow(() => parseMessageRequestV1(bodies[0]));
});

test("REQ-UI-002: unbound options are refused locally; clarification expiry stays core-authoritative", async () => {
  const card = await currentCard("clarification_four_options");
  let calls = 0;
  await withServer((request, response) => {
    calls++;
    response.writeHead(409, { "content-type": "application/json" });
    response.end(JSON.stringify({ error: { code: "INTERNAL_SAFETY_BLOCK", message: "synthetic refusal" } }));
  }, async (baseUrl) => {
    assert.equal(await submitClarification({ baseUrl, conversationRef: conversation, sessionToken: bearer, card, optionId: "opt_unknown", requestId: cardRequestId }), "unavailable");
    assert.equal(await submitClarification({ baseUrl, conversationRef: conversation, sessionToken: bearer, card, optionId: card.options[0].option_id, requestId: "invalid" }), "unavailable");
    // The client validates the UTC instant's shape, not whether the device clock says it has expired.
    // The core owns expiry evaluation; a server refusal is surfaced as a safe unavailable result.
    assert.equal(await submitClarification({ baseUrl, conversationRef: conversation, sessionToken: bearer, card: { ...card, expires_at: "2000-01-01T00:00:00Z" }, optionId: card.options[0].option_id, requestId: cardRequestId }), "unavailable");
    assert.equal(await submitClarification({ baseUrl, conversationRef: conversation, sessionToken: bearer, card: { ...card, expires_at: "2099-99-99T00:00:00Z" }, optionId: card.options[0].option_id, requestId: cardRequestId }), "unavailable");
    assert.equal(await submitClarification({ baseUrl, conversationRef: conversation, sessionToken: bearer, card: { ...card, expires_at: "2099-02-31T00:00:00Z" }, optionId: card.options[0].option_id, requestId: cardRequestId }), "unavailable");
  });
  assert.equal(calls, 1);
});

test("REQ-UI-002: actual 503 and a closed socket produce safe failure, not server text", async () => {
  const card = await currentCard("confirmation");
  const envelope = await readFile(new URL("../../contracts/fixtures/valid/errors.v3/dependency_unavailable_retryable.json", import.meta.url));
  await withServer((request, response) => {
    response.writeHead(503, { "content-type": "application/json" });
    response.end(envelope);
  }, async (baseUrl) => {
    assert.equal(await submitConfirmation({ baseUrl, sessionToken: bearer, card, response: "ACCEPT" }), "unavailable");
  });
  await withServer((request) => request.socket.destroy(), async (baseUrl) => {
    assert.equal(await submitConfirmation({ baseUrl, sessionToken: bearer, card, response: "ACCEPT" }), "unavailable");
  });
});


test("REQ-UI-002 US-18: a 409 CONFIRMATION_EXPIRED envelope is terminal, not retryable", async () => {
  const card = await currentCard("confirmation");
  const envelope = {
    error: {
      code: "CONFIRMATION_EXPIRED",
      message: "This confirmation has expired.",
      retryable: false,
      correlation_id: "corr_4c21f9",
    },
  };
  await withServer((_request, response) => {
    response.writeHead(409, { "content-type": "application/json" });
    response.end(JSON.stringify(envelope));
  }, async (baseUrl) => {
    assert.equal(await submitConfirmation({ baseUrl, sessionToken: bearer, card, response: "ACCEPT" }), "expired");
  });
});

test("REQ-UI-002 US-18: malformed or oversized 409 bodies fail closed as unavailable", async () => {
  const card = await currentCard("confirmation");
  await withServer((_request, response) => {
    response.writeHead(409, { "content-type": "application/json" });
    response.end(JSON.stringify({ error: { code: "CONFIRMATION_EXPIRED", message: "x", retryable: false } }));
  }, async (baseUrl) => {
    assert.equal(await submitConfirmation({ baseUrl, sessionToken: bearer, card, response: "ACCEPT" }), "unavailable");
  });
  await withServer((_request, response) => {
    response.writeHead(409, { "content-type": "application/json" });
    response.end(" ".repeat(65_537));
  }, async (baseUrl) => {
    assert.equal(await submitConfirmation({ baseUrl, sessionToken: bearer, card, response: "ACCEPT" }), "unavailable");
  });
});


test("A715: the None of these choice submits only the core-minted none_option_id", async () => {
  const card = await currentCard("clarification_four_options");
  assert.equal(typeof card.none_option_id, "string");
  assert.ok(card.none_option_id.length > 0);
  let seen;
  await withServer(async (request, response) => {
    seen = { path: request.url, auth: request.headers.authorization, body: await bodyOf(request) };
    response.writeHead(202, { "content-type": "application/json" }).end();
  }, async (baseUrl) => {
    const result = await submitClarification({
      baseUrl, conversationRef: conversation, sessionToken: bearer, card,
      optionId: card.none_option_id, requestId: cardRequestId,
    });
    assert.equal(result, "accepted");
  });
  assert.equal(seen.path, `/v1/conversations/${conversation}/messages`);
  assert.equal(seen.auth, `Bearer ${bearer}`);
  assert.equal(seen.body.option_id, card.none_option_id);
  assert.equal(seen.body.clarification_id, card.clarification_id);
  assert.deepEqual(Object.keys(seen.body).sort(), ["clarification_id", "option_id", "request_id"]);
});
