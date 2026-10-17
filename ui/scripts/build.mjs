import { cpSync, mkdirSync } from "node:fs";

mkdirSync("dist", { recursive: true });
cpSync("index.html", "dist/index.html");
cpSync("build/js", "dist/js", { recursive: true });
