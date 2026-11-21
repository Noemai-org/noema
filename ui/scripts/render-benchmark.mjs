// Bench-only static rendering entry, for synthetic fixtures while the generated
// conditional validator and bench-profile access boundary remain outstanding.
// This file does not run in the customer UI or contact a core route.
import { readFile } from "node:fs/promises";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { ScorecardInspector } from "../build/js/scorecard-inspector.js";

let scorecard;
try {
  if (process.argv.length !== 3) throw new Error("A scorecard file is required");
  scorecard = JSON.parse(await readFile(process.argv[2], "utf8"));
} catch {
  // Never print file paths, input text, or error details.
  scorecard = null;
}
process.stdout.write(renderToStaticMarkup(createElement(ScorecardInspector, { scorecard })));
