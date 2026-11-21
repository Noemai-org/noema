import { cpSync, mkdirSync, rmSync } from "node:fs";
import { readFileSync, writeFileSync } from "node:fs";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { Overlay } from "../build/js/overlay.js";
import { build } from "esbuild";

rmSync("dist", { recursive: true, force: true });
mkdirSync("dist", { recursive: true });
const template = readFileSync("index.html", "utf8");
const app = renderToStaticMarkup(createElement(Overlay));
writeFileSync("dist/index.html", template.replace("<!-- OVERLAY -->", app));
cpSync("src/tokens.css", "dist/tokens.css");
await build({
  entryPoints: ["src/browser.ts"],
  outfile: "dist/app.js",
  bundle: true,
  format: "esm",
  platform: "browser",
  target: ["es2022"],
  define: { "process.env.NODE_ENV": '"production"' },
});
