import test from "node:test";
import assert from "node:assert/strict";
import { startUiFailureServer } from "../test-support/ui-failure-server.mjs";

// Real local HTTP and sockets; the server is synthetic and is not the core.
test("UI fault server produces actual connection and HTTP failures", async () => {
  for (const scenario of ["unavailable", "malformed", "disconnect", "oversized", "wrong-content-type"]) {
    const server = await startUiFailureServer({ scenario });
    try {
      const url = `${server.origin}/v1/investigations/inv_SyntheticA`;
      const options = { headers: { authorization: "Bearer synthetic-session" } };
      if (scenario === "disconnect") { await assert.rejects(fetch(url, options)); continue; }
      const response = await fetch(url, options);
      assert.equal(response.status, scenario === "unavailable" ? 503 : 200);
      const body = await response.text();
      if (scenario === "unavailable") assert.equal(JSON.parse(body).error.message, "HOSTILE_SERVER_DIAGNOSTIC");
      if (scenario === "malformed") assert.throws(() => JSON.parse(body));
      if (scenario === "oversized") assert.ok(Buffer.byteLength(body) > 65536);
      if (scenario === "wrong-content-type") assert.match(response.headers.get("content-type"), /text\/plain/u);
    } finally { await server.close(); }
  }
});
