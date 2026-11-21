// The module graph of the built UI, for the isolation tests (A258, A433; noema#798): which modules a built module reaches through its relative imports. Test support only.
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join, posix, sep } from "node:path";

// The module specifiers of one built JS source, and how many import() calls take an argument that is not a string literal (those cannot be followed).
export function specifiersOf(source) {
  return { specifiers: [...source.matchAll(/from "([^"]+)"/g)].map((m) => m[1]), computed: 0 };
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
