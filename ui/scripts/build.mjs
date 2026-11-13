import { cpSync, mkdirSync } from "node:fs";
import { readFileSync, writeFileSync } from "node:fs";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { Overlay } from "../build/js/overlay.js";

mkdirSync("dist", { recursive: true });
const template = readFileSync("index.html", "utf8");
const app = renderToStaticMarkup(createElement(Overlay));
writeFileSync("dist/index.html", template.replace("<!-- OVERLAY -->", app));
cpSync("src/tokens.css", "dist/tokens.css");
cpSync("build/js", "dist/js", { recursive: true });
