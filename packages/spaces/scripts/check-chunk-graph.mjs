// Fail the package build when the emitted chunk graph is one a host's bundler
// can break.
//
// A host installs the package and re-bundles dist/ with its own Vite/Rollup.
// That is safe only while every dynamic `import()` in dist/ resolves to the
// target chunk's own namespace. When a module is imported both statically and
// dynamically, Rollup puts it in a shared chunk and emits the dynamic import as
// a namespace accessor, `import("./shared.js").then((m) => m.i)`. Vite's
// dynamic-import tree-shaking rewrites `const { x } = await import(...)` as if
// `x` were a direct export of the chunk and ignores the trailing `.then`, so
// the destructure reads `undefined` and throws once re-bundled.
//
// vite.config.ts turns such imports static in the library build; this check
// proves the result:
//
//   1. no dynamic import in dist/ goes through a namespace accessor;
//   2. no chunk under dist/chunks/ is both dynamically imported and statically
//      imported by another chunk. Static edges from the entry files (dist/*.js)
//      are allowed: an entry is the public facade of its chunk, and a module an
//      entry exposes keeps its own chunk and export names.
//
// The emitted code is Rollup output, so the import forms are regular: static
// imports are `import ... from "./x.js"` / `export ... from "./x.js"`, dynamic
// ones `import("./x.js")` with a literal relative specifier.
import { readFileSync, readdirSync, existsSync } from "node:fs";
import { dirname, join, posix, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const STATIC_IMPORT =
  /\b(?:import|export)\b(?!\s*\()\s+(?:[^"'();]*?\bfrom\s+)?["'](\.\.?\/[^"']+)["']/g;
const DYNAMIC_IMPORT = /\bimport\(\s*["'](\.\.?\/[^"']+)["']\s*\)/g;
const NAMESPACE_ACCESSOR =
  /\bimport\(\s*["'](\.\.?\/[^"']+)["']\s*\)\s*\.then\(\s*\(?\s*([\w$]+)\s*\)?\s*=>\s*\2\.([\w$]+)\s*\)/g;

/**
 * Check a set of emitted files, keyed by their path relative to dist/
 * (posix separators). Returns the problems found; empty means the graph is
 * safe to re-bundle.
 */
export function chunkGraphProblems(files) {
  const problems = [];
  const staticImporters = new Map();
  const dynamicImporters = new Map();
  const add = (map, target, importer) => {
    if (!map.has(target)) map.set(target, new Set());
    map.get(target).add(importer);
  };

  for (const [file, source] of Object.entries(files)) {
    const from = posix.dirname(file);
    for (const [, spec] of source.matchAll(STATIC_IMPORT)) {
      add(staticImporters, posix.join(from, spec), file);
    }
    for (const [, spec] of source.matchAll(DYNAMIC_IMPORT)) {
      add(dynamicImporters, posix.join(from, spec), file);
    }
    for (const [, spec, , member] of source.matchAll(NAMESPACE_ACCESSOR)) {
      problems.push(
        `${file}: import("${spec}") reads namespace member "${member}" — the target module shares a chunk ` +
          "(it is also imported statically), and a re-bundling host loses that member",
      );
    }
  }

  for (const [target, dynamic] of dynamicImporters) {
    const chunkImporters = [...(staticImporters.get(target) ?? [])].filter((file) =>
      file.startsWith("chunks/"),
    );
    if (chunkImporters.length > 0) {
      problems.push(
        `${target} is imported dynamically (${[...dynamic].join(", ")}) and statically ` +
          `(${chunkImporters.join(", ")})`,
      );
    }
  }
  return problems;
}

/** Read every emitted JS file under `distDir` (top level and chunks/). */
export function readDist(distDir) {
  const files = {};
  for (const sub of ["", "chunks"]) {
    const dir = join(distDir, sub);
    if (!existsSync(dir)) continue;
    for (const name of readdirSync(dir)) {
      if (name.endsWith(".js")) {
        files[sub ? `${sub}/${name}` : name] = readFileSync(join(dir, name), "utf-8");
      }
    }
  }
  return files;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const distDir = resolve(dirname(fileURLToPath(import.meta.url)), "..", "dist");
  const files = readDist(distDir);
  if (Object.keys(files).length === 0) {
    console.error(`check-chunk-graph: no built JS under ${distDir}`);
    process.exit(1);
  }
  const problems = chunkGraphProblems(files);
  if (problems.length > 0) {
    console.error("check-chunk-graph: the emitted chunk graph breaks when a host re-bundles it:");
    for (const problem of problems) console.error(`  - ${problem}`);
    console.error(
      "A module imported both statically and dynamically must not keep its dynamic edge " +
        "(see staticSharedDynamicImports in vite.config.ts).",
    );
    process.exit(1);
  }
  console.log(`check-chunk-graph: ${Object.keys(files).length} files, no shared dynamic imports`);
}
