// Local contract server, not the core. Synthetic fixture data only; no live services.
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";

async function fixture(path) {
  return readFile(new URL(`../../contracts/fixtures/valid/${path}.json`, import.meta.url), "utf8");
}

export async function startContractServer({ socketPath, scenario }) {
  if (!["normal", "error", "drop"].includes(scenario)) throw new Error("Unknown test scenario");
  const [investigation, progress, unavailable, notFound] = await Promise.all([
    fixture("investigation-response/understanding"), fixture("sse-event/progress"),
    fixture("errors.v3/dependency_unavailable_retryable"), fixture("errors.v3/not_found_or_not_authorized"),
  ]);
  const server = createServer((req, res) => {
    if (req.method === "GET" && /^\/v1\/investigations\/[^/]+$/u.test(req.url ?? "")) {
      res.writeHead(scenario === "error" ? 503 : 200, { "content-type": "application/json" });
      res.end(scenario === "error" ? unavailable : investigation);
      return;
    }
    if (req.method === "GET" && /^\/v1\/conversations\/[^/]+\/events$/u.test(req.url ?? "")) {
      res.writeHead(200, { "content-type": "text/event-stream" });
      if (scenario === "drop") {
        res.write("event: progress\ndata: ");
        res.socket.destroy();
      } else {
        res.end(`event: progress\ndata: ${JSON.stringify(JSON.parse(progress))}\n\n`);
      }
      return;
    }
    res.writeHead(404, { "content-type": "application/json" });
    res.end(notFound);
  });
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(socketPath, resolve);
  });
  return server;
}
