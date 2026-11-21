// The module graph of the built UI, for the isolation tests (A258, A433; noema#798): which modules a built module reaches through its relative imports. Test support only.
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join, posix, sep } from "node:path";

// The module specifiers of one built JS source, and how many import() calls take an argument that is not a plain string literal (those cannot be followed, so a caller fails closed on them).
// Followed: `from "x"` (static imports and re-exports, over any number of lines), a bare side-effect `import "x"`, and a dynamic `import("x")` with a literal in any quote. Comments and strings
// are not stripped: a specifier named in a comment counts as an import, which can only make a test stricter, never let an import through.
const FROM = /\bfrom\s*(["'`])([^"'`\n]+)\1/g;
const BARE = /\bimport\s*(["'`])([^"'`\n]+)\1/g;
const DYNAMIC_LITERAL = /\bimport\s*\(\s*(["'`])([^"'`$\n]+)\1\s*[,)]/g;
const DYNAMIC_ANY = /\bimport\s*\(/g;

export function specifiersOf(source) {
  const specifiers = [];
  for (const re of [FROM, BARE, DYNAMIC_LITERAL]) for (const m of source.matchAll(re)) specifiers.push(m[2]);
  const literal = [...source.matchAll(DYNAMIC_LITERAL)].length;
  return { specifiers, computed: Math.max(0, [...source.matchAll(DYNAMIC_ANY)].length - literal) };
}

// The modules reachable from the entries through relative imports. `read(path)` gives a module's source, or null when it is not there. Paths are posix and relative to the build root.
export function reach(read, entries) {
  const reached = new Set();
  const missing = [];
  const computed = [];
  const todo = [...entries];
  while (todo.length) {
    const file = todo.pop();
    if (reached.has(file)) continue;
    reached.add(file);
    const source = read(file);
    if (source === null) {
      missing.push(file);
      continue;
    }
    const found = specifiersOf(source);
    if (found.computed > 0) computed.push(file);
    for (const spec of found.specifiers) {
      if (spec.startsWith("./") || spec.startsWith("../")) todo.push(posix.normalize(posix.join(posix.dirname(file), spec)));
    }
  }
  return { reached, missing, computed };
}

export const dirReader = (root) => (file) => (existsSync(join(root, file)) ? readFileSync(join(root, file), "utf8") : null);
export const mapReader = (modules) => (file) => (Object.hasOwn(modules, file) ? modules[file] : null);
// Every built module under root, posix paths relative to it.
export const modulesOf = (root) => readdirSync(root, { recursive: true }).map((f) => f.split(sep).join("/")).filter((f) => f.endsWith(".js"));
