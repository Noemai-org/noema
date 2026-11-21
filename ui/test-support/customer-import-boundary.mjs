import { readFile, realpath } from "node:fs/promises";
import { dirname, extname, isAbsolute, relative, resolve, sep } from "node:path";
import ts from "typescript";

// Test-only: walk actual import/export syntax, including side effects and dynamic
// imports. Computed imports and unresolved local modules fail closed.
export async function customerImportViolations(root, roots) {
  const base = await realpath(root);
  const pending = roots.map((item) => resolve(base, item));
  const seen = new Set();
  const findings = [];
  const inside = (path) => { const rel = relative(base, path); return rel !== ".." && !rel.startsWith(`..${sep}`) && !isAbsolute(rel); };
  async function resolveSource(from, specifier) {
    const target = resolve(dirname(from), specifier);
    const stem = /\.(?:m?js|cjs)$/u.test(target) ? target.replace(/\.(?:m?js|cjs)$/u, "") : target;
    const choices = extname(target) && stem === target ? [target] : [`${stem}.ts`, `${stem}.tsx`, `${stem}.mts`, target, resolve(target, "index.ts")];
    for (const candidate of choices) {
      try { return await realpath(candidate); } catch (error) { if (error?.code !== "ENOENT" && error?.code !== "ENOTDIR") throw error; }
    }
    return undefined;
  }
  while (pending.length) {
    let file;
    try { file = await realpath(pending.pop()); } catch (error) { if (error?.code === "ENOENT") { findings.push("missing-local-module"); continue; } throw error; }
    if (!inside(file)) { findings.push("outside-source-root"); continue; }
    if (file === resolve(base, "contract_types", "approver.ts")) { findings.push("approver-entry"); continue; }
    if (seen.has(file)) continue;
    seen.add(file);
    const source = ts.createSourceFile(file, await readFile(file, "utf8"), ts.ScriptTarget.Latest, true);
    const specs = [];
    const visit = (node) => {
      if ((ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) && node.moduleSpecifier) specs.push(node.moduleSpecifier);
      if (ts.isCallExpression(node) && (node.expression.kind === ts.SyntaxKind.ImportKeyword || (ts.isIdentifier(node.expression) && node.expression.text === "require"))) specs.push(node.arguments[0]);
      ts.forEachChild(node, visit);
    };
    visit(source);
    for (const spec of specs) {
      if (!spec || !ts.isStringLiteralLike(spec)) { findings.push("computed-import"); continue; }
      if (!spec.text.startsWith(".")) continue;
      const next = await resolveSource(file, spec.text);
      if (!next) findings.push("missing-local-module"); else pending.push(next);
    }
  }
  return findings;
}
