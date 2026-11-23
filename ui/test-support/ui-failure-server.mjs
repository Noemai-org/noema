// Local synthetic fault server, not the core. The built UI is the subject under test.
import { createServer } from "node:http";
import { networkInterfaces } from "node:os";
import { readFile } from "node:fs/promises";

const assets = new Map([
  ["/", ["dist/index.html", "text/html"]],
  ["/tokens.css", ["dist/tokens.css", "text/css"]],
  ["/app.js", ["dist/app.js", "text/javascript"]],
]);

export async function startUiFailureServer({ scenario }) {
  if (!["unavailable", "malformed", "disconnect", "oversized", "wrong-content-type"].includes(scenario)) throw new Error("Unknown synthetic scenario");
  const address = Object.values(networkInterfaces()).flat().find((entry) => entry?.internal && entry.family === "IPv4")?.address;
  if (!address) throw new Error("No local IPv4 interface");
  const envelope = JSON.parse(await readFile(new URL("../../contracts/fixtures/valid/errors.v3/dependency_unavailable_retryable.json", import.meta.url), "utf8"));
  envelope.error.message = "HOSTILE_SERVER_DIAGNOSTIC";
  const server = createServer(async (request, response) => {
    if (request.method === "GET" && request.url === "/v1/investigations/inv_SyntheticA") {
      if (request.headers.authorization !== "Bearer synthetic-session" || request.headers.cookie) { response.writeHead(401).end(); return; }
      if (scenario === "disconnect") { request.socket.destroy(); return; }
      if (scenario === "unavailable") { response.writeHead(503, { "content-type": "application/json" }).end(JSON.stringify(envelope)); return; }
      if (scenario === "wrong-content-type") { response.writeHead(200, { "content-type": "text/plain" }).end("HOSTILE_SERVER_DIAGNOSTIC"); return; }
      response.writeHead(200, { "content-type": "application/json" });
      response.end(scenario === "oversized" ? " ".repeat(65537) : "{malformed HOSTILE_SERVER_DIAGNOSTIC");
      return;
    }
    const asset = request.method === "GET" && assets.get(request.url);
    if (!asset) { response.writeHead(404).end(); return; }
    try { const body = await readFile(asset[0]); response.writeHead(200, { "content-type": asset[1] }).end(body); }
    catch { response.writeHead(404).end(); }
  });
  await new Promise((resolve, reject) => { server.once("error", reject); server.listen(0, address, resolve); });
  return {
    origin: `http://${address}:${server.address().port}`,
    close: async () => { server.closeAllConnections(); await new Promise((resolve) => server.close(resolve)); },
  };
}
