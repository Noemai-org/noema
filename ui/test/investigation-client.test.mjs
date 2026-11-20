import test from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { networkInterfaces } from "node:os";
import { once } from "node:events";
import { readFile } from "node:fs/promises";
import { fetchInvestigation, isSecureInvestigationTarget } from "../build/js/investigation-client.js";

// Local contract server, not the core. All data, including the session, is SYNTHETIC.
const investigationId = "inv_SyntheticA";
const sessionToken = "synthetic-session";
const maxResponseBytes = 16 * 1024; // Synthetic test limit, not a production policy.

test("REQ-UI-001: refetch will not send a bearer to a non-loopback HTTP origin", () => {
  assert.equal(isSecureInvestigationTarget(new URL("http://example.invalid")), false);
  assert.equal(isSecureInvestigationTarget(new URL("http://127.0.0.1.example.invalid")), false);
  assert.equal(isSecureInvestigationTarget(new URL("https://user@example.invalid")), false);
  assert.equal(isSecureInvestigationTarget(new URL("https://example.invalid")), true);
  assert.equal(isSecureInvestigationTarget(new URL("http://localhost")), true);
});

async function withServer(handler, run) {
  const address = Object.values(networkInterfaces()).flat().find((item) => item?.internal && item.family === "IPv4")?.address;
  assert.ok(address, "A loopback IPv4 interface is required");
  const server = createServer(handler);
  server.listen(0, address);
  await once(server, "listening");
  try { await run(`http://${address}:${server.address().port}`); }
  finally { server.closeAllConnections(); await new Promise((resolve) => server.close(resolve)); }
}

test("REQ-UI-001: refetches the current contract state with the host session", async () => {
  const answer = JSON.parse(await readFile(new URL("../../contracts/fixtures/valid/investigation-response/completed_with_answer.json", import.meta.url), "utf8"));
  let observed;
  await withServer((request, response) => {
    observed = { method: request.method, path: request.url, auth: request.headers.authorization, cookie: request.headers.cookie };
    response.writeHead(200, { "content-type": "application/json" });
    response.end(JSON.stringify(answer));
  }, async (baseUrl) => {
    assert.deepEqual(await fetchInvestigation({ baseUrl, investigationId, sessionToken, maxResponseBytes }), answer);
  });
  assert.deepEqual(observed, { method: "GET", path: `/v1/investigations/${investigationId}`, auth: `Bearer ${sessionToken}`, cookie: undefined });
});

test("REQ-UI-001: a real 503 and malformed body never create an answer or echo server text", async () => {
  const fixture = await readFile(new URL("../../contracts/fixtures/valid/investigation-response/completed_with_answer.json", import.meta.url));
  const envelope = await readFile(new URL("../../contracts/fixtures/valid/errors.v3/dependency_unavailable_retryable.json", import.meta.url));
  await withServer((request, response) => {
    if (request.url.endsWith("/inv_Accepted")) {
      response.writeHead(202, { "content-type": "application/json" });
      response.end(fixture);
    } else if (request.url.endsWith("/inv_Big")) {
      response.writeHead(200, { "content-type": "application/json" });
      response.end("x".repeat(maxResponseBytes + 1));
    } else if (request.url.endsWith("/inv_Bad")) {
      response.writeHead(200, { "content-type": "application/json" });
      response.end("{bad json");
    } else {
      response.writeHead(503, { "content-type": "application/json" });
      response.end(envelope);
    }
  }, async (baseUrl) => {
    for (const id of [investigationId, "inv_Bad", "inv_Big", "inv_Accepted"]) {
      await assert.rejects(fetchInvestigation({ baseUrl, investigationId: id, sessionToken, maxResponseBytes }), (error) => {
        assert.ok(!String(error).includes("DEPENDENCY_UNAVAILABLE"));
        return true;
      });
    }
  });
});
