// @sprout-foundry/workspace build-artifact tests
// ==============================================
// The package is the hosted artifact: a host installs `dist/` and nothing
// else. These tests assert what that artifact contains — the entry, its
// lazily loaded chunks, the type declarations and the publish allowlist —
// and that the scaffold wiring (workspaces, the `make build-all` step) is in
// place.
//
// The assertions read `packages/workspace/dist`, produced by the package's
// own build. `make install` runs the workspace-package build before this file
// and `make build-all` runs it as part of the full build, so the artifact is
// always fresh where these tests matter. A missing `dist/` (a bare `make lint`
// on a fresh checkout) skips the artifact assertions with a message instead of
// failing on an artifact the test did not produce.
//
// Run with: node docs/__tests__/workspace-package.test.js

import { test, describe } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath, pathToFileURL } from "node:url";

// ── Helpers ────────────────────────────────────────────────────────────

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const ROOT = path.resolve(__dirname, "../..");
const PACKAGE_DIR = path.join(ROOT, "packages/workspace");
const DIST = path.join(PACKAGE_DIR, "dist");

const read = (rel) => fs.readFileSync(path.join(ROOT, rel), "utf-8");
const readJson = (rel) => JSON.parse(read(rel));
const exists = (rel) => fs.existsSync(path.join(ROOT, rel));

/** Every file under dist/, as posix paths relative to packages/workspace. */
function walk(dir, acc = []) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) walk(full, acc);
    else acc.push(path.relative(PACKAGE_DIR, full).split(path.sep).join("/"));
  }
  return acc;
}

/** The built entry points and every `.d.ts`, from the manifest. */
function declaredArtifacts(pkg) {
  const files = [];
  for (const spec of pkg.files) {
    if (spec.endsWith("/")) continue;
    files.push(spec);
  }
  // `exports` targets are either a conditions object (`{types, import}`) or a
  // bare string (a stylesheet subpath such as `./styles.css`); flatten both.
  for (const target of Object.values(pkg.exports)) {
    const values =
      typeof target === "string" ? [target] : Object.values(target ?? {});
    for (const value of values) files.push(value.replace(/^\.\//, ""));
  }
  if (pkg.types) files.push(pkg.types.replace(/^\.\//, ""));
  if (pkg.module) files.push(pkg.module.replace(/^\.\//, ""));
  return [
    ...new Set(
      files.map((file) => (file.startsWith("dist/") ? file : `dist/${file}`)),
    ),
  ];
}

const pkg = readJson("packages/workspace/package.json");
const rootPkg = readJson("package.json");
const built = fs.existsSync(DIST);
if (!built) {
  console.warn(
    "[workspace-package] packages/workspace/dist is absent — skipping artifact assertions. " +
      "Run `npm run build -w @sprout-foundry/workspace` (or `make build-all`) first.",
  );
}

/**
 * The static import specifiers of a built JS file: `import ... from "..."`,
 * bare `import "..."` and `export ... from "..."`. A dynamic `import("...")`
 * is deliberately NOT matched — that is a lazy boundary, and a chunk reached
 * only through one is not in the importer's static graph. The `(?!\s*\()` on
 * the keyword keeps `import(` out even when the call's argument sits on the
 * next line.
 */
function staticSpecifiers(source) {
  const pattern =
    /\b(?:import|export)\b(?!\s*\()\s+(?:[^"'();]*?\bfrom\s+)?["']([^"']+)["']/g;
  return [...source.matchAll(pattern)].map((m) => m[1]);
}

/**
 * Every relative file statically reachable from a built entry, following the
 * `./chunks/...` specifiers transitively. This is what "importing the
 * package" means: the browser (or a bundler of the host) fetches this set
 * eagerly when the entry module is evaluated.
 */
function staticImportGraph(entryName, seen = new Set()) {
  const abs = path.join(PACKAGE_DIR, entryName);
  if (!fs.existsSync(abs) || fs.statSync(abs).isDirectory()) return seen;
  if (seen.has(entryName)) return seen;
  seen.add(entryName);
  for (const spec of staticSpecifiers(fs.readFileSync(abs, "utf-8"))) {
    if (spec.startsWith(".")) {
      // Normalize to posix so the keys match `walk()`'s chunk names (which
      // split on `path.sep`): on Windows `path.relative` yields backslashes,
      // and a `graph.has(file)` identity check against forward-slash names
      // would silently never match.
      const rel = path
        .relative(PACKAGE_DIR, path.resolve(path.dirname(abs), spec))
        .split(path.sep)
        .join("/");
      staticImportGraph(rel, seen);
    }
  }
  return seen;
}

/** Markers that identify editor (CodeMirror) code in a built chunk. */
const EDITOR_MARKERS = /@codemirror\/|CodeMirror|cm-editor/;
/** Markers that identify the WASM agent (its loader / instantiation). */
const WASM_MARKERS = /sprout\.wasm|wasm_exec|WebAssembly\.instantiate/;

/** The package's one emitted stylesheet (`exports["./styles.css"]`). */
const STYLESHEET = "dist/workspace.css";

/** Read the emitted stylesheet, comment-stripped (the xterm license banner lives in a comment). */
function readSheet() {
  return stripCssComments(
    fs.readFileSync(path.join(PACKAGE_DIR, STYLESHEET), "utf-8"),
  );
}

/** Remove CSS comments (respecting strings) so a scan never parses comment prose as a selector. */
function stripCssComments(css) {
  let out = "";
  let i = 0;
  while (i < css.length) {
    const ch = css[i];
    if (ch === '"' || ch === "'") {
      const quote = ch;
      out += ch;
      i += 1;
      while (i < css.length && css[i] !== quote) {
        if (css[i] === "\\") {
          out += css[i];
          i += 1;
        }
        if (i < css.length) {
          out += css[i];
          i += 1;
        }
      }
      if (i < css.length) {
        out += css[i];
        i += 1;
      }
      continue;
    }
    if (ch === "/" && css[i + 1] === "*") {
      i += 2;
      while (i < css.length && !(css[i] === "*" && css[i + 1] === "/")) i += 1;
      i += 2;
      continue;
    }
    out += ch;
    i += 1;
  }
  return out;
}

/**
 * The selector-list preludes in a comment-free stylesheet, at every nesting
 * depth (so a `body` rule inside `@media` is seen). At-rule preludes
 * (`@media …`, `@keyframes …`) are dropped, and the whole body of an
 * `@keyframes` block is skipped — its stanzas (`from`, `to`, `0%`, `0%, 100%`)
 * are animation steps, not selectors that match host elements. The scan tracks
 * the open-block stack for the same reason the scoping pass does: a step list
 * can carry spaces and commas (`0%, 100%`) that a per-prelude regex misses.
 */
function selectorPreludes(css) {
  const preludes = [];
  const stack = [];
  let pending = "";
  let i = 0;
  while (i < css.length) {
    const ch = css[i];
    if (ch === '"' || ch === "'") {
      const quote = ch;
      pending += ch;
      i += 1;
      while (i < css.length && css[i] !== quote) {
        pending += css[i];
        if (css[i] === "\\") {
          i += 1;
          if (i < css.length) pending += css[i];
        }
        i += 1;
      }
      if (i < css.length) {
        pending += css[i];
        i += 1;
      }
      continue;
    }
    if (ch === "{") {
      const prelude = pending.trim();
      if (!prelude.startsWith("@") && !stack.includes("keyframes")) {
        for (const part of prelude.split(",")) {
          const selector = part.trim();
          if (selector) preludes.push(selector);
        }
      }
      stack.push(/^@keyframes/.test(prelude) ? "keyframes" : "rule");
      pending = "";
    } else if (ch === "}") {
      stack.pop();
      pending = "";
    } else {
      pending += ch;
    }
    i += 1;
  }
  return preludes;
}

/**
 * Selectors that would escape the workspace root and style the host page.
 *
 * The check is the same predicate the scoping pass uses (`isLeakingSelector`):
 * a selector leaks when it has no workspace-root class anchor and its first
 * compound is not class-scoped — i.e. it reaches host-owned elements by name
 * (`body`, `select`), by the universal `*`, or by a bare attribute
 * (`[data-theme=dark] .x`). Reusing one predicate (rather than a list of
 * literal anchors) is what makes this a proof: a new leak form the pass does
 * not scope is a leak this test reports.
 */
async function globalLeaks(css) {
  const { isLeakingSelector } = await loadScopeStylesheet();
  return selectorPreludes(css).filter(
    // `isLeakingSelector` is the pass's own predicate; a `:root` that somehow
    // survived the rewrite is a leak too (the pass rewrites every `:root` to
    // the workspace-root class), so flag it explicitly as a belt-and-braces.
    (selector) =>
      selector === ":root" ||
      selector.startsWith(":root") ||
      isLeakingSelector(selector),
  );
}

/**
 * Load the scoping pass the build runs. It is ESM and lives under
 * `packages/workspace/scripts/`, so it is imported by URL.
 */
async function loadScopeStylesheet() {
  return import(
    pathToFileURL(path.join(PACKAGE_DIR, "scripts/scope-stylesheet.mjs")).href
  );
}

/**
 * The scoped prelude of a one-rule stylesheet — a small harness for the
 * rewrite cases below.
 */
function scopeSelectorList(css) {
  return css.replace(/\{[^}]*\}$/, "");
}

// ── Stylesheet scoping pass (ws.5) ─────────────────────────────────────

describe("stylesheet scoping pass", () => {
  test("scopes a host-rooted selector instead of passing it through", async () => {
    // The enforcement must not treat "contains the workspace-root class" as
    // "already scoped": `body .sprout-workspace .panel` starts at a host
    // element and would style the host page. Only a selector whose FIRST
    // compound is the workspace root is already scoped.
    const { scopeStylesheet } = await loadScopeStylesheet();
    assert.equal(
      scopeSelectorList(
        scopeStylesheet("body .sprout-workspace .panel{color:red}"),
      ),
      ".sprout-workspace body .sprout-workspace .panel",
    );
  });

  test("leaves an already workspace-rooted selector byte-for-byte", async () => {
    const { scopeStylesheet } = await loadScopeStylesheet();
    for (const selector of [
      ".sprout-workspace .panel",
      ".sprout-workspace[data-theme=light] .x",
      ".sprout-workspace.extra",
    ]) {
      assert.equal(
        scopeSelectorList(scopeStylesheet(`${selector}{color:red}`)),
        selector,
      );
    }
  });

  test("rescopes :root token/theme blocks onto the workspace root", async () => {
    const { scopeStylesheet } = await loadScopeStylesheet();
    assert.equal(
      scopeSelectorList(scopeStylesheet(":root{--x:1}")),
      ".sprout-workspace",
    );
    assert.equal(
      scopeSelectorList(
        scopeStylesheet(":root[data-theme='light'] .logo{fill:red}"),
      ),
      ".sprout-workspace[data-theme='light'] .logo",
    );
    assert.equal(
      scopeSelectorList(scopeStylesheet(":root.phone-tab-bar-shown{--y:2}")),
      ".sprout-workspace.phone-tab-bar-shown",
    );
  });

  test("scopes bare element and universal selectors, keeps class-scoped ones", async () => {
    const { scopeStylesheet } = await loadScopeStylesheet();
    assert.equal(
      scopeSelectorList(scopeStylesheet("body{color:red}")),
      ".sprout-workspace body",
    );
    assert.equal(
      scopeSelectorList(scopeStylesheet("*{margin:0}")),
      ".sprout-workspace *",
    );
    assert.equal(
      scopeSelectorList(scopeStylesheet("select option{color:red}")),
      ".sprout-workspace select option",
    );
    assert.equal(
      scopeSelectorList(scopeStylesheet("select.styled-select{color:red}")),
      "select.styled-select",
    );
    assert.equal(
      scopeSelectorList(scopeStylesheet(".themed-dialog-body{color:red}")),
      ".themed-dialog-body",
    );
    // A bare attribute selector has no class anchor, so it too reaches
    // host-owned elements (a host root may carry `data-theme`) and is scoped.
    assert.equal(
      scopeSelectorList(scopeStylesheet("[data-theme=dark] .x{color:red}")),
      ".sprout-workspace [data-theme=dark] .x",
    );
  });

  test("never scopes keyframe steps, including multi-step and whitespace forms", async () => {
    const { scopeStylesheet } = await loadScopeStylesheet();
    for (const source of [
      "@keyframes spin{from{opacity:0}to{opacity:1}50%{opacity:.5}}",
      "@keyframes a{0%, 100% {opacity:0}}",
      "@keyframes a{0%,\n100%{opacity:0}}",
      "@keyframes a{0%,80%,to{opacity:0}40%{opacity:.4}}",
      "@keyframes shake { 0%,\n100% { transform: translate(0) } }",
    ]) {
      assert.equal(
        scopeStylesheet(source),
        source,
        `keyframe body must pass through verbatim: ${source}`,
      );
    }
  });

  test("scopes a rule after a keyframes block (keyframe context does not leak)", async () => {
    const { scopeStylesheet } = await loadScopeStylesheet();
    assert.equal(
      scopeStylesheet("@keyframes a{0%, 100% {opacity:0}} body{color:red}"),
      "@keyframes a{0%, 100% {opacity:0}} .sprout-workspace body{color:red}",
    );
  });

  test("strips comments and never parses comment prose as a selector", async () => {
    const { scopeStylesheet } = await loadScopeStylesheet();
    const scoped = scopeStylesheet(
      "/* body { color: red } to deal */ .panel{color:red}",
    );
    assert.ok(
      !scoped.includes("body"),
      "comment prose is not parsed as a selector",
    );
    assert.equal(scoped, ".panel{color:red}");
  });
});

// ── Package manifest ───────────────────────────────────────────────────

describe("@sprout-foundry/workspace package.json", () => {
  test("is published to GitHub Packages like @sprout-foundry/design", () => {
    const design = readJson("packages/design/package.json");
    assert.equal(pkg.name, "@sprout-foundry/workspace");
    assert.equal(pkg.publishConfig.registry, design.publishConfig.registry);
    assert.equal(pkg.license, design.license);
    assert.equal(pkg.repository.type, design.repository.type);
    assert.equal(pkg.repository.url, design.repository.url);
  });

  test("is an ESM package with a build script", () => {
    assert.equal(pkg.type, "module");
    assert.match(pkg.scripts.build, /vite build/);
  });

  test("exports the entry with types, ESM only", () => {
    const entry = pkg.exports["."];
    assert.ok(entry, 'exports has a "." entry');
    assert.equal(entry.import, "./dist/index.js");
    assert.equal(entry.types, "./dist/index.d.ts");
    assert.equal(entry.require, undefined, "the package is ESM only");
  });

  test("bundles the internal packages from the workspace (dev dependencies only)", () => {
    for (const name of ["@sprout/events", "@sprout/ui"]) {
      assert.ok(
        pkg.devDependencies[name],
        `builds against ${name} from the workspace`,
      );
      assert.ok(
        !pkg.dependencies?.[name],
        `${name} is bundled, not installed by the host`,
      );
    }
  });
});

// ── Lockstep version ───────────────────────────────────────────────────

describe("version lockstep with the sprout release", () => {
  test("the package version equals the root sprout version", () => {
    assert.equal(
      pkg.version,
      rootPkg.version,
      "packages/workspace/package.json must track the root sprout version",
    );
  });

  test("npm workspaces resolve the package (glob covers packages/*)", () => {
    assert.ok(
      rootPkg.workspaces.includes("packages/*"),
      "the workspace glob covers the package",
    );
  });

  test("the lockfile records the package at the sprout version", () => {
    const lock = readJson("package-lock.json");
    const entry = lock.packages?.["packages/workspace"];
    assert.ok(entry, "package-lock.json has a packages/workspace entry");
    assert.equal(entry.name, pkg.name);
    assert.equal(entry.version, rootPkg.version);
  });
});

// ── Build wiring ───────────────────────────────────────────────────────

describe("build wiring", () => {
  test("the Makefile builds the package and depends on it from build-all", () => {
    const makefile = read("Makefile");
    assert.match(
      makefile,
      /^build-workspace-package:/m,
      "Makefile has a build-workspace-package target",
    );
    assert.match(
      makefile,
      /^build-all: .*\bbuild-workspace-package\b/m,
      "build-all depends on build-workspace-package",
    );
  });

  test("the package is built after its dependencies (events, ui, webui)", () => {
    const makefile = read("Makefile");
    const target = makefile.match(/^build-workspace-package:(.*)$/m);
    assert.ok(target, "build-workspace-package has a prerequisite list");
    assert.match(
      target[1],
      /^[ \t]*deploy-ui\b/,
      "build-workspace-package depends on deploy-ui (which builds @sprout/events and @sprout/ui first)",
    );
  });
});

// ── Source entry points ────────────────────────────────────────────────

describe("package sources", () => {
  test("the entry re-exports the public host entry point", () => {
    const source = read("packages/workspace/src/index.ts");
    assert.match(source, /export \* from '\.\/host';/);
  });

  test("the views entry point re-exports the public views module", () => {
    const source = read("packages/workspace/src/viewsChunk.ts");
    assert.match(source, /from '\.\.\/\.\.\/\.\.\/webui\/src\/views\/index'/);
  });

  test("the providers entry point re-exports the provider wrapper", () => {
    const source = read("packages/workspace/src/providersChunk.ts");
    assert.match(
      source,
      /from "\.\.\/\.\.\/\.\.\/webui\/src\/providers\/SproutProviders"/,
    );
    assert.match(source, /SproutProviders/);
  });

  test("the host entry point re-exports the public host module", () => {
    const source = read("packages/workspace/src/host.ts");
    assert.match(source, /from '\.\.\/\.\.\/\.\.\/webui\/src\/host\/index'/);
  });
});

// ── Build artifacts ────────────────────────────────────────────────────

describe(
  "build artifacts",
  { skip: !built && "packages/workspace/dist not built" },
  () => {
    test("the declared entry points and type declarations were emitted", () => {
      for (const file of declaredArtifacts(pkg)) {
        if (file === "dist/README.md") continue;
        assert.ok(
          fs.existsSync(path.join(PACKAGE_DIR, file)),
          `missing emitted artifact: ${file}`,
        );
      }
    });

    test("no unexpected files in dist/ (the publish allowlist is exact)", () => {
      // Every emitted file must be covered by the allowlist: the declared
      // artifacts, the chunks directory, the stylesheet, the WASM directory and
      // the README. An emitted file the allowlist does not cover would be
      // dropped by npm pack, so the artifact would ship an incomplete package.
      const allowed = new Set([
        ...declaredArtifacts(pkg),
        "dist/workspace.css",
        "dist/README.md",
      ]);
      const isAllowed = (file) =>
        allowed.has(file) ||
        file.startsWith("dist/chunks/") ||
        file.startsWith("dist/wasm/");
      const unexpected = walk(DIST).filter((file) => !isAllowed(file));
      assert.deepEqual(
        unexpected,
        [],
        `dist/ has files the allowlist does not cover: ${unexpected.join(", ")}`,
      );
      // The WASM directory must actually be emitted, not merely tolerated.
      assert.ok(
        fs.existsSync(path.join(DIST, "wasm")),
        "the build emits dist/wasm/",
      );

      // The wasm directory itself is exact: the manifest, the fixed-name
      // fallbacks and the files the manifest names — no stale hashed asset left
      // over from a previous build.
      const wasmDir = path.join(DIST, "wasm");
      const manifest = JSON.parse(
        fs.readFileSync(path.join(wasmDir, "wasm-manifest.json"), "utf-8"),
      );
      const expectedWasmFiles = new Set([
        "wasm-manifest.json",
        "sprout.wasm",
        "wasm_exec.js",
        ...Object.values(manifest.files ?? {}),
      ]);
      const stale = fs
        .readdirSync(wasmDir)
        .filter((name) => !expectedWasmFiles.has(name));
      assert.deepEqual(
        stale,
        [],
        `dist/wasm/ has stale or unexpected files: ${stale.join(", ")}`,
      );
    });

    test("the entry declares the host contract and the package version", () => {
      const types = fs.readFileSync(path.join(DIST, "index.d.ts"), "utf-8");
      assert.match(
        types,
        /SproutHost/,
        "the declaration names the host contract",
      );
      assert.match(
        types,
        /WORKSPACE_PACKAGE_NAME/,
        "the declaration names the package identity",
      );
      assert.match(
        types,
        /WORKSPACE_PACKAGE_VERSION/,
        "the declaration names the package version",
      );
    });

    test("the flat declaration resolves its own relative re-exports", () => {
      // The published entry is dist/index.d.ts and the declaration tree it was
      // flattened from lives under dist/chunks/declarations. A relative
      // specifier left pointing at a sibling that is no longer there (e.g.
      // './host') is a broken `exports.types`, so every relative import in the
      // entry must resolve on disk.
      const types = fs.readFileSync(path.join(DIST, "index.d.ts"), "utf-8");
      const specifiers = [
        ...types.matchAll(/(?:from|import\()\s*'(\.[^']+)'/g),
      ].map((m) => m[1]);
      for (const spec of specifiers) {
        const target = path.join(DIST, spec);
        assert.ok(
          fs.existsSync(target) || fs.existsSync(`${target}.d.ts`),
          `the entry's relative specifier does not resolve: ${spec}`,
        );
      }
    });

    test("the version constants are declared exactly once", () => {
      const types = fs.readFileSync(path.join(DIST, "index.d.ts"), "utf-8");
      const declarations = [
        ...types.matchAll(/declare const (WORKSPACE_PACKAGE_\w+)/g),
      ].map((m) => m[1]);
      assert.deepEqual(
        [...declarations].sort(),
        ["WORKSPACE_PACKAGE_NAME", "WORKSPACE_PACKAGE_VERSION"],
        "each version constant is declared exactly once",
      );
    });

    test("the entry JS resolves its chunks relatively (no bare first-party imports)", () => {
      const entry = fs.readFileSync(path.join(DIST, "index.js"), "utf-8");
      const specifiers = [...entry.matchAll(/from\s+"([^"]+)"/g)].map(
        (m) => m[1],
      );
      assert.ok(specifiers.length > 0, "the entry imports at least one chunk");
      for (const spec of specifiers) {
        assert.ok(spec.startsWith("./"), `entry import is relative: ${spec}`);
      }
    });

    test("code-splitting: the entry is small and the views are their own chunks", () => {
      const entry = fs.statSync(path.join(DIST, "index.js")).size;
      const views = fs.statSync(path.join(DIST, "views.js")).size;
      const providers = fs.statSync(path.join(DIST, "providers.js")).size;
      assert.ok(
        entry < 16 * 1024,
        `the entry must stay small, was ${entry} bytes`,
      );
      assert.ok(
        views < 16 * 1024,
        `the views facade must stay small, was ${views} bytes`,
      );
      assert.ok(
        providers < 16 * 1024,
        `the providers facade must stay small, was ${providers} bytes`,
      );

      const chunks = walk(DIST).filter((file) =>
        /^dist\/chunks\/.*\.js$/.test(file),
      );
      assert.ok(
        chunks.length > 0,
        "the build emitted at least one lazily loaded chunk",
      );
      const heavy = chunks.some(
        (file) => fs.statSync(path.join(PACKAGE_DIR, file)).size > 64 * 1024,
      );
      assert.ok(
        heavy,
        "at least one chunk carries the heavy views (the split is real)",
      );
    });

    test("no unexpected files are emitted (dist is within the publish allowlist)", () => {
      const allowedExact = new Set(declaredArtifacts(pkg));
      const allowedPrefixes = pkg.files
        .filter((spec) => spec.endsWith("/"))
        .map((spec) => spec);
      const unexpected = walk(DIST).filter((file) => {
        if (allowedExact.has(file)) return false;
        return !allowedPrefixes.some((prefix) => file.startsWith(prefix));
      });
      assert.deepEqual(
        unexpected,
        [],
        `unexpected files in dist/: ${unexpected.join(", ")}`,
      );
    });

    test("the artifact ships no source maps or test files", () => {
      const offenders = walk(DIST).filter((file) =>
        /\.map$|\.test\.|\.spec\./.test(file),
      );
      assert.deepEqual(
        offenders,
        [],
        `unexpected files in dist/: ${offenders.join(", ")}`,
      );
    });

    test("the emitted stylesheet is at its declared stable path and carries the views styles", () => {
      // ws.5 (SP-160 §160a): the styles are a first-class, explicitly-named
      // artifact — `dist/workspace.css`, reachable as
      // `@sprout-foundry/workspace/styles.css` — not an incidental
      // `chunks/*.css` emitted because a `.css` import happened to be reachable.
      // Assert the content survived the library build and the scoping pass.
      assert.ok(
        fs.existsSync(path.join(DIST, "workspace.css")),
        "the build emits dist/workspace.css",
      );
      const css = readSheet();
      assert.ok(
        css.includes(".sprout-views-layout"),
        "the emitted stylesheet carries the layout styles",
      );
      assert.ok(
        css.includes(".themed-dialog-overlay"),
        "the emitted stylesheet carries the component styles",
      );
    });

    test("the stylesheet leaks no global CSS outside the workspace root", async () => {
      // ws.5 (SP-160 §160a): loading the stylesheet on a host page must not style
      // the host's DOM outside the mounted workspace. No selector may survive
      // that would match a host-owned element — the app's token block, its
      // theme guards, its element resets and its bare-attribute rules are all
      // re-scoped to the workspace root class the `SproutWorkspace` root
      // renders. The predicate is the scoping pass's own, so a new leak form is
      // reported here rather than passing silently.
      const leaks = await globalLeaks(readSheet());
      assert.deepEqual(
        leaks,
        [],
        `the stylesheet leaks global selectors onto the host page: ${leaks.join(", ")}`,
      );
    });

    test("the stylesheet keeps the workspace-root scope the views mount into", () => {
      // The other side of the same property: scoping must not delete the
      // workspace-rooted rules. The views root class is still styled, and the
      // light-theme logo guards that used to be written on `:root` are now
      // guarded on the workspace root instead.
      const css = readSheet();
      assert.ok(
        css.includes(".sprout-views-layout"),
        "the workspace-rooted layout styles survive scoping",
      );
      assert.ok(
        /\.sprout-workspace\[data-theme=light\]/.test(css),
        "the light-theme guards are scoped to the workspace root",
      );
    });
  },
);

// ── Content-hashed WASM (SP-160 §160e, Acceptance criteria 6) ──────────

describe(
  "the package ships content-hashed WASM and a manifest",
  { skip: !built && "packages/workspace/dist not built" },
  () => {
    // "Upgrading the package changes the WASM asset URLs": the package emits
    // sprout.<hash>.wasm / wasm_exec.<hash>.js plus wasm-manifest.json under
    // dist/wasm/, the same shape the loader (webui/src/services/wasmShell.ts)
    // reads. The names are content-derived (the shared hashing helper in
    // scripts/build-webui-dist.mjs over the source bytes), so a changed binary
    // changes the URL a host resolves. `./wasm/` is a declared subpath export,
    // so a host serves the directory without guessing its path.
    const WASM_DIR = path.join(DIST, "wasm");
    const manifestPath = path.join(WASM_DIR, "wasm-manifest.json");

    test("dist/wasm/ is emitted with a manifest and the hashed assets", () => {
      assert.ok(
        fs.existsSync(manifestPath),
        "the build emits dist/wasm/wasm-manifest.json",
      );
      const manifest = JSON.parse(fs.readFileSync(manifestPath, "utf-8"));
      assert.equal(
        manifest.version,
        1,
        "the manifest carries the loader's version",
      );

      for (const logical of ["sprout.wasm", "wasm_exec.js"]) {
        const hashed = manifest.files?.[logical];
        assert.ok(hashed, `the manifest names a hashed ${logical}`);
        assert.ok(
          fs.existsSync(path.join(WASM_DIR, hashed)),
          `the manifest's ${logical} → ${hashed} exists on disk`,
        );
      }
    });

    test("the manifest's names match the emitted files exactly", () => {
      const manifest = JSON.parse(fs.readFileSync(manifestPath, "utf-8"));
      const emitted = fs.readdirSync(WASM_DIR);
      // Every name the manifest points at is present in the emitted directory.
      for (const name of Object.values(manifest.files ?? {})) {
        assert.ok(
          emitted.includes(name),
          `the manifest name ${name} is an emitted file`,
        );
      }
      assert.ok(
        emitted.includes("wasm-manifest.json"),
        "the manifest is emitted in dist/wasm/",
      );
    });

    test("the hashed names are derived from the source WASM content", async () => {
      // Recompute the hash from the source bytes with the SAME helper the build
      // uses: the emitted name is provably content-derived, not incidental.
      const { buildWasmManifest } = await import(
        pathToFileURL(path.join(ROOT, "scripts/build-webui-dist.mjs")).href
      );
      const sourceDir = path.join(ROOT, "webui/public/wasm");
      const entries = {};
      for (const logical of ["sprout.wasm", "wasm_exec.js"]) {
        const from = path.join(sourceDir, logical);
        if (fs.existsSync(from)) entries[logical] = fs.readFileSync(from);
      }
      assert.ok(
        entries["sprout.wasm"],
        "webui/public/wasm/sprout.wasm is present (run `make build-wasm`)",
      );
      const expected = buildWasmManifest(entries);
      const manifest = JSON.parse(fs.readFileSync(manifestPath, "utf-8"));
      assert.equal(
        manifest.wasm,
        expected.wasm,
        "the emitted wasm name is the source content's hash",
      );
      assert.equal(
        manifest.wasmExec,
        expected.wasmExec,
        "the emitted wasmExec name is the source content's hash",
      );
    });

    test("the names are the standard hashed shape the loader resolves", () => {
      const manifest = JSON.parse(fs.readFileSync(manifestPath, "utf-8"));
      assert.match(manifest.wasm, /^sprout\.[0-9a-f]{10}\.wasm$/);
      assert.match(manifest.wasmExec, /^wasm_exec\.[0-9a-f]{10}\.js$/);
    });

    test("the package declares the wasm/ directory as the publish allowlist entry", () => {
      // The emitted directory must be in `files` so a host installing the
      // package actually receives it (the artifact test's "no unexpected files"
      // check is the other half), and reachable through a subpath export so a
      // host can resolve its path without hard-coding the layout.
      assert.ok(
        pkg.files.includes("dist/wasm/"),
        "package.json files includes dist/wasm/",
      );
      assert.equal(
        pkg.exports["./wasm/"],
        "./dist/wasm/",
        "package.json exports a ./wasm/ subpath",
      );
    });
  },
);

// ── Lazy loading (SP-160 Acceptance criteria 5) ────────────────────────

describe(
  "lazy loading: the entry loads no editor or WASM code",
  { skip: !built && "packages/workspace/dist not built" },
  () => {
    // "Importing the package loads no editor or WASM code until a space opens":
    // importing is evaluating `dist/index.js`, which eager-fetches exactly the
    // static import graph below (transitively). Dynamic imports are the lazy
    // boundary — a chunk reached only through one is NOT in this set.
    const graph = staticImportGraph("dist/index.js");
    const contents = [...graph].map((file) => [
      file,
      fs.readFileSync(path.join(PACKAGE_DIR, file), "utf-8"),
    ]);

    // The chunks that carry the editor / WASM loader, identified across the whole
    // artifact. The size- and identity-based assertions below do not depend on
    // the markers surviving minification, so a re-bundled editor cannot slip past
    // by having its literals renamed.
    const allChunks = walk(DIST).filter(
      (file) => file.startsWith("dist/chunks/") && file.endsWith(".js"),
    );
    const chunkSources = allChunks.map((file) => [
      file,
      fs.readFileSync(path.join(PACKAGE_DIR, file), "utf-8"),
    ]);
    const heavyChunks = chunkSources
      .filter(
        ([, source]) =>
          EDITOR_MARKERS.test(source) || WASM_MARKERS.test(source),
      )
      .map(([file]) => file);

    test("the entry graph reaches files (the walk is real, not vacuous)", () => {
      assert.ok(
        graph.size >= 2,
        `the entry statically imports at least one chunk, reached ${graph.size}`,
      );
    });

    test("no file in the entry graph contains editor code", () => {
      const offenders = contents
        .filter(([, source]) => EDITOR_MARKERS.test(source))
        .map(([file]) => file);
      assert.deepEqual(
        offenders,
        [],
        `editor code is eagerly imported by the entry: ${offenders.join(", ")}`,
      );
    });

    test("no file in the entry graph contains the WASM loader", () => {
      const offenders = contents
        .filter(([, source]) => WASM_MARKERS.test(source))
        .map(([file]) => file);
      assert.deepEqual(
        offenders,
        [],
        `the WASM loader is eagerly imported by the entry: ${offenders.join(", ")}`,
      );
    });

    test("no file in the entry graph is a heavy chunk (a renamed editor cannot evade the marker scan)", () => {
      // Minification concatenates and renames modules, so a chunk that carries
      // the editor could in principle contain no `CodeMirror` literal. A size
      // ceiling is independent of naming: the editor bundle is hundreds of
      // kilobytes, so any eager file above this cap is a regression whatever it
      // is called. This is the assertion that does not depend on the markers.
      const CAP = 64 * 1024;
      const offenders = contents
        .map(([file]) => [file, fs.statSync(path.join(PACKAGE_DIR, file)).size])
        .filter(([, size]) => size > CAP)
        .map(([file, size]) => `${file} (${size} bytes)`);
      assert.deepEqual(
        offenders,
        [],
        `the entry eagerly loads a heavy chunk: ${offenders.join(", ")}`,
      );
    });

    test("the editor and WASM chunks exist but are NOT in the entry graph", () => {
      // The split must be real, not a deletion of the functionality: the editor
      // and the WASM agent are still built, in chunks the entry does not
      // statically import. Asserting the known-heavy chunks are absent from the
      // static graph checks the boundary by chunk identity, not by re-reading
      // markers the minifier could have renamed.
      assert.ok(
        heavyChunks.length > 0,
        "a chunk carries the editor or WASM loader",
      );
      const eagerlyReached = heavyChunks.filter((file) => graph.has(file));
      assert.deepEqual(
        eagerlyReached,
        [],
        `a heavy chunk is in the entry's static graph: ${eagerlyReached.join(", ")}`,
      );
    });

    test("the heavy chunks are loaded by the views/providers entries, not the package entry", () => {
      // The lazy boundary in this package is the entry point, not a dynamic
      // import: a host imports the `views` (or `providers`) entry when it mounts
      // a space, and that entry pulls the heavy chunks. Prove the split is real
      // by chunk identity — at least one heavy chunk is statically reachable from
      // the `views` entry and none from the package entry (asserted above).
      const viewsGraph = staticImportGraph("dist/views.js");
      const reachableFromViews = heavyChunks.filter((file) =>
        viewsGraph.has(file),
      );
      assert.ok(
        reachableFromViews.length > 0,
        `the views entry must load the heavy chunks (heavy: ${heavyChunks.join(", ")})`,
      );
    });
  },
);

// ── No platform calls on import ────────────────────────────────────────

describe(
  "no platform calls on import",
  { skip: !built && "packages/workspace/dist not built" },
  () => {
    // Importing the package must not reach the bootstrap adapter (its module
    // scope fetches /api/bootstrap and installs an adapter) nor resolve platform
    // entitlements (the cloud host's /billing/status). Both used to sit in the
    // entry's static graph and run at module scope; the resolved data now arrives
    // through the host's transport instead, and entitlements resolve lazily.
    const graph = staticImportGraph("dist/index.js");
    const sources = [...graph].map((file) => [
      file,
      fs.readFileSync(path.join(PACKAGE_DIR, file), "utf-8"),
    ]);

    test("the entry graph reaches files (the walk is real, not vacuous)", () => {
      assert.ok(
        graph.size >= 2,
        `the entry statically imports at least one chunk, reached ${graph.size}`,
      );
    });

    test("no file in the entry graph carries the bootstrap-adapter fetch on import", () => {
      const offenders = sources
        .filter(
          ([, source]) =>
            /bootstrapAdapter/.test(source) || /\/api\/bootstrap/.test(source),
        )
        .map(([file]) => file);
      assert.deepEqual(
        offenders,
        [],
        `the bootstrap adapter is in the entry graph: ${offenders.join(", ")}`,
      );
    });

    test("importing the built entry performs no fetch", () => {
      // The behavioral guard: a child node process stubs global.fetch before
      // importing the built entry, then asserts no call was made at import time.
      // Out-of-process keeps the runner's own module cache and globals untouched,
      // and the assertion is exactly what a host observes. A module-scope
      // /api/bootstrap or /billing/status call would make `calls` non-empty.
      const probe = `
      const calls = [];
      globalThis.fetch = (...args) => { calls.push(args); return Promise.resolve({ ok: false }); };
      await import(${JSON.stringify(pathToFileURL(path.join(PACKAGE_DIR, "dist/index.js")).href)});
      await new Promise((resolve) => setTimeout(resolve, 300));
      process.stdout.write(String(calls.length));
    `;
      const result = spawnSync(
        process.execPath,
        ["--input-type=module", "-e", probe],
        {
          encoding: "utf-8",
          timeout: 30_000,
        },
      );
      assert.equal(
        result.status,
        0,
        `the entry import probe exited ${result.status}: ${result.stderr}`,
      );
      assert.equal(
        result.stdout.trim(),
        "0",
        "importing the package performed a fetch at module scope",
      );
    });
  },
);

describe(
  "source is not published",
  { skip: !built && "packages/workspace/dist not built" },
  () => {
    test("the publish allowlist never includes src/ or the vite config", () => {
      for (const spec of pkg.files) {
        assert.ok(
          !spec.startsWith("src"),
          `files must not publish source: ${spec}`,
        );
        assert.ok(
          !spec.includes("tsconfig") && !spec.includes("vite.config"),
          `files must not publish config: ${spec}`,
        );
      }
    });
  },
);

// ── Installable by a host ──────────────────────────────────────────────

describe("a host can install the published package", () => {
  test("no installed dependency uses a file: or link: specifier", () => {
    for (const field of [
      "dependencies",
      "peerDependencies",
      "optionalDependencies",
    ]) {
      for (const [name, spec] of Object.entries(pkg[field] ?? {})) {
        assert.ok(
          !/^(file|link):/.test(spec),
          `${field}.${name} is not installable from a registry: ${spec}`,
        );
      }
    }
  });

  test("react and react-dom are peer dependencies, never regular ones", () => {
    for (const name of ["react", "react-dom"]) {
      assert.ok(pkg.peerDependencies?.[name], `${name} is a peer dependency`);
      assert.ok(
        !pkg.dependencies?.[name],
        `${name} must not be a regular dependency`,
      );
    }
  });
});

describe(
  "the build keeps peer dependencies external",
  { skip: !built && "packages/workspace/dist not built" },
  () => {
    const scripts = () => walk(DIST).filter((file) => file.endsWith(".js"));

    test("no React internals are bundled into dist/", () => {
      const offenders = scripts().filter((file) => {
        const source = fs.readFileSync(path.join(PACKAGE_DIR, file), "utf-8");
        return /__SECRET_INTERNALS_DO_NOT_USE|ReactCurrentOwner|react\.production\.min/.test(
          source,
        );
      });
      assert.deepEqual(
        offenders,
        [],
        `React is bundled into: ${offenders.join(", ")}`,
      );
    });

    test("React is imported from the host by bare specifier", () => {
      const imports = scripts().flatMap((file) => {
        const source = fs.readFileSync(path.join(PACKAGE_DIR, file), "utf-8");
        return [
          ...source.matchAll(/from\s*["'](react(?:-dom)?(?:\/[^"']*)?)["']/g),
        ].map((m) => m[1]);
      });
      assert.ok(
        imports.includes("react"),
        "the build imports react from the host",
      );
    });

    test(
      "declarations resolve without paths outside the package",
      { todo: "declarations are not yet bundled (SP-160 §160e)" },
      () => {
        const declarations = walk(DIST).filter((file) =>
          file.endsWith(".d.ts"),
        );
        const offenders = declarations.filter((file) => {
          const source = fs.readFileSync(path.join(PACKAGE_DIR, file), "utf-8");
          return /from\s*["'](?:(?:\.\.\/)+webui\/|@sprout-foundry\/workspace-webui)/.test(
            source,
          );
        });
        assert.deepEqual(
          offenders,
          [],
          `declarations reference files outside the package: ${offenders.join(", ")}`,
        );
      },
    );
  },
);
