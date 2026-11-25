// Consumer smoke: a host installs the packed tarball and builds with it.
// =====================================================================
// The strongest artifact assertion: everything else checks `dist/` in the
// repo, but a host consumes the *packed* package. This script `npm pack`s
// `packages/spaces`, stands the tarball up as an installed dependency of a
// scratch TypeScript + Vite **React 18** app created outside the repo (in
// `os.tmpdir()`), and requires:
//
//   - `tsc --noEmit` to type-check the app importing `SproutSpaces` (from
//     `@sprout-foundry/spaces/views`), `SproutProviders` (from
//     `@sprout-foundry/spaces/providers`) and the host types (from
//     `@sprout-foundry/spaces`) — proving the subpath `exports` map and the
//     rolled-up `views.d.ts`/`providers.d.ts` resolve from the tarball alone;
//   - `vite build` to bundle the app;
//   - the built app to contain a SINGLE React copy.
//
// Robustness over fidelity, deliberately: `npm install`ing the tarball plus
// react/vite/typescript into the scratch app would need the network (a fresh
// registry fetch of the peer + toolchain), which is flaky and can hang. The
// app's `node_modules` is instead populated by symlinking the repo's own
// installed packages (react 18, react-dom, @types/react(-dom), vite,
// @vitejs/plugin-react, typescript) and linking `@sprout-foundry/spaces` at
// the EXTRACTED tarball — the same module resolution a real install produces,
// with no network. `npm pack` (the packaging step under test) is still run for
// real, and the tarball's contents are asserted.
//
// The single-React-copy check is the module graph, not a string count: a Vite
// plugin records every resolved `react`/`react-dom` module id, and the script
// asserts they all resolve to ONE install root. A package that bundled its own
// React would surface a second root under its `dist/`.
//
// Run with: node --test docs/__tests__/spaces-consumer.test.mjs

import { test, describe, before, after } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const ROOT = path.resolve(__dirname, "../..");
const PACKAGE_DIR = path.join(ROOT, "packages/spaces");
const DIST = path.join(PACKAGE_DIR, "dist");
const NODE_MODULES = path.join(ROOT, "node_modules");

const built = fs.existsSync(DIST);
if (!built) {
  console.warn(
    "[spaces-consumer] packages/spaces/dist is absent — skipping. " +
      "Run `npm run build -w @sprout-foundry/spaces` (or `make build-all`) first.",
  );
}

/** Run a command, returning the spawn result with both streams. */
function run(command, args, options = {}) {
  let executable = command;
  let useShell = false;
  if (process.platform === "win32") {
    if (command === "npm") {
      // npm is npm.cmd, which a shell resolves via PATHEXT (same approach
      // as scripts/build-webui-dist.mjs).
      executable = "npm.cmd";
      useShell = true;
    } else if (command === "tar") {
      // Pin System32's bsdtar: the checked-out PATH may resolve Git's GNU
      // tar, which reads the leading "C:" of a Windows path as a remote
      // hostname ("Cannot connect to C: resolve failed").
      executable = path.join(
        process.env.SystemRoot || "C:\\Windows",
        "System32",
        "tar.exe",
      );
    }
  }
  return spawnSync(executable, args, {
    encoding: "utf-8",
    maxBuffer: 64 * 1024 * 1024,
    shell: useShell,
    ...options,
  });
}

/**
 * Populate `nodeModulesDir` with symlinks to the repo's installed packages so
 * the scratch app resolves React, the toolchain and their transitive
 * dependencies without a network install. `@sprout-foundry/spaces` is the
 * exception: it points at the extracted tarball.
 */
function linkNodeModules(nodeModulesDir, extractedPackageDir) {
  fs.mkdirSync(nodeModulesDir, { recursive: true });
  for (const name of fs.readdirSync(NODE_MODULES)) {
    // `@sprout-foundry` is re-pointed at the tarball below, not the repo.
    if (name === "@sprout-foundry") continue;
    const target = path.join(nodeModulesDir, name);
    if (fs.lstatSync(target, { throwIfNoEntry: false })) continue;
    // "junction": unprivileged on Windows, a dir symlink elsewhere.
    fs.symlinkSync(path.join(NODE_MODULES, name), target, "junction");
  }
  // The spaces package, installed from the tarball rather than the repo.
  const scope = path.join(nodeModulesDir, "@sprout-foundry");
  fs.mkdirSync(scope, { recursive: true });
  const spacesLink = path.join(scope, "spaces");
  if (!fs.lstatSync(spacesLink, { throwIfNoEntry: false })) {
    fs.symlinkSync(extractedPackageDir, spacesLink, "junction");
  }
}

const SCRATCH = built
  ? fs.mkdtempSync(path.join(os.tmpdir(), "spaces-consumer-"))
  : null;
const APP = SCRATCH ? path.join(SCRATCH, "app") : null;
const PACK_DIR = SCRATCH ? path.join(SCRATCH, "pack") : null;
const EXTRACT_DIR = SCRATCH ? path.join(SCRATCH, "unpacked") : null;
let tarballPath = null;

after(() => {
  if (SCRATCH) fs.rmSync(SCRATCH, { recursive: true, force: true });
});

describe(
  "a host consumes the packed tarball",
  { skip: !built && "packages/spaces/dist not built" },
  () => {
    before(() => {
      fs.mkdirSync(PACK_DIR, { recursive: true });
      fs.mkdirSync(EXTRACT_DIR, { recursive: true });

      // `npm pack` the package exactly as a publish would (from the package
      // dir, honouring the `files` allowlist and the `exports` map).
      const packed = run(
        "npm",
        ["pack", "--pack-destination", PACK_DIR, "--silent"],
        { cwd: PACKAGE_DIR },
      );
      assert.equal(
        packed.status,
        0,
        `npm pack failed:\n${packed.stdout}${packed.stderr}`,
      );
      const tarballs = fs
        .readdirSync(PACK_DIR)
        .filter((name) => name.endsWith(".tgz"));
      assert.equal(tarballs.length, 1, "npm pack emitted one tarball");
      tarballPath = path.join(PACK_DIR, tarballs[0]);

      const extracted = run("tar", ["xzf", tarballPath, "-C", EXTRACT_DIR]);
      assert.equal(
        extracted.status,
        0,
        `extracting the tarball failed:\n${extracted.stdout}${extracted.stderr}`,
      );
    });

    test("the tarball ships every exports target, including the subpath declarations", () => {
      const packageDir = path.join(EXTRACT_DIR, "package");
      const manifest = JSON.parse(
        fs.readFileSync(path.join(packageDir, "package.json"), "utf-8"),
      );
      // Each `exports` target must exist in the packed tarball — a target the
      // `files` allowlist omitted would ship a broken specifier.
      const targets = [];
      const flatten = (value) => {
        if (typeof value === "string") targets.push(value);
        else if (value && typeof value === "object")
          for (const nested of Object.values(value)) flatten(nested);
      };
      for (const value of Object.values(manifest.exports)) flatten(value);
      for (const target of targets) {
        const file = path.join(packageDir, target);
        assert.ok(
          fs.existsSync(file),
          `the tarball is missing an exports target: ${target}`,
        );
      }
      for (const subpath of ["./views", "./providers"]) {
        const entry = manifest.exports[subpath];
        assert.ok(entry, `the tarball declares the ${subpath} export`);
        assert.ok(entry.types, `${subpath} declares types`);
        assert.ok(entry.import, `${subpath} declares the ESM import`);
        for (const rel of [entry.types, entry.import]) {
          assert.ok(
            fs.existsSync(path.join(packageDir, rel)),
            `the tarball is missing ${subpath} ${rel}`,
          );
        }
      }
    });

    test("a scratch React 18 app type-checks, builds, and bundles a single React copy", () => {
      const packageDir = path.join(EXTRACT_DIR, "package");

      // ── The scratch app ──────────────────────────────────────────────
      const srcDir = path.join(APP, "src");
      fs.mkdirSync(srcDir, { recursive: true });
      linkNodeModules(path.join(APP, "node_modules"), packageDir);

      fs.writeFileSync(
        path.join(APP, "package.json"),
        JSON.stringify(
          {
            name: "spaces-consumer",
            private: true,
            type: "module",
            version: "0.0.0",
          },
          null,
          2,
        ),
      );
      fs.writeFileSync(
        path.join(APP, "index.html"),
        [
          "<!doctype html>",
          '<html lang="en">',
          '  <head><meta charset="UTF-8" /><title>consumer</title></head>',
          "  <body>",
          '    <div id="root"></div>',
          '    <script type="module" src="/src/main.tsx"></script>',
          "  </body>",
          "</html>",
          "",
        ].join("\n"),
      );

      // Imports every public path the composition API promises: the views
      // (SproutSpaces) and the space registry from `./views`, the provider
      // wrapper from `./providers`, and the host contract types from `.`.
      fs.writeFileSync(
        path.join(srcDir, "main.tsx"),
        [
          'import { createRoot } from "react-dom/client";',
          "import {",
          "  SproutSpaces,",
          "  SproutProviders,",
          "  availableModes,",
          "  registerSpace,",
          '} from "@sprout-foundry/spaces/views";',
          'import { SproutProviders as ProvidersOnly } from "@sprout-foundry/spaces/providers";',
          'import { localHost } from "@sprout-foundry/spaces";',
          'import type { SproutHost } from "@sprout-foundry/spaces";',
          'import type { SproutSpacesProps } from "@sprout-foundry/spaces/views";',
          'import type { SproutProvidersProps } from "@sprout-foundry/spaces/providers";',
          'import "@sprout-foundry/spaces/styles.css";',
          "",
          "const host: SproutHost = localHost;",
          "void registerSpace;",
          "void availableModes({ hasMockup: false });",
          "",
          "const providersProps: SproutProvidersProps = { children: null };",
          "void providersProps;",
          "",
          "function App() {",
          "  const props: SproutSpacesProps = {",
          '    project: { id: "demo" },',
          '    space: "code",',
          "    host,",
          "  };",
          "  return (",
          "    <ProvidersOnly>",
          "      <SproutProviders>",
          "        <SproutSpaces {...props} />",
          "      </SproutProviders>",
          "    </ProvidersOnly>",
          "  );",
          "}",
          "",
          'createRoot(document.getElementById("root")!).render(<App />);',
          "",
        ].join("\n"),
      );

      fs.writeFileSync(
        path.join(APP, "tsconfig.json"),
        JSON.stringify(
          {
            compilerOptions: {
              target: "ES2020",
              lib: ["DOM", "DOM.Iterable", "ES2020"],
              module: "ESNext",
              moduleResolution: "bundler",
              jsx: "react-jsx",
              strict: true,
              noEmit: true,
              skipLibCheck: true,
              esModuleInterop: true,
            },
            include: ["src"],
          },
          null,
          2,
        ),
      );

      // ── tsc --noEmit ─────────────────────────────────────────────────
      // `node <typescript/bin/tsc>`: the `.bin/tsc` shim is a POSIX shell
      // script CreateProcess cannot execute on Windows (see
      // packages/spaces/scripts/bundle-dts.mjs).
      const require = createRequire(import.meta.url);
      const tsc = require.resolve("typescript/bin/tsc");
      const typeCheck = run(
        process.execPath,
        [tsc, "--noEmit", "-p", "tsconfig.json"],
        {
          cwd: APP,
        },
      );
      assert.equal(
        typeCheck.status,
        0,
        `a host importing the packed package failed to type-check:\n${typeCheck.stdout}${typeCheck.stderr}`,
      );

      // ── vite build ───────────────────────────────────────────────────
      // The plugin records every resolved react/react-dom module id into
      // `react-modules.json`; the assertion below reads it.
      fs.writeFileSync(
        path.join(APP, "vite.config.mjs"),
        [
          'import { defineConfig } from "vite";',
          'import react from "@vitejs/plugin-react";',
          'import { writeFileSync } from "node:fs";',
          "",
          "const reactModules = new Set();",
          "",
          "export default defineConfig({",
          "  plugins: [",
          "    react(),",
          "    {",
          '      name: "record-react-modules",',
          "      moduleParsed(info) {",
          '        const id = info.id.split("?")[0];',
          "        if (/(?:^|\\/)node_modules\\/(react|react-dom)\\//.test(id)) {",
          '          reactModules.add(id.replace(/^\\0/, ""));',
          "        }",
          "      },",
          "      buildEnd() {",
          '        writeFileSync("react-modules.json", JSON.stringify([...reactModules], null, 2));',
          "      },",
          "    },",
          "  ],",
          '  build: { outDir: "dist", emptyOutDir: true },',
          "});",
          "",
        ].join("\n"),
      );

      const viteBin = path.join(NODE_MODULES, "vite/bin/vite.js");
      const build = run(
        process.execPath,
        [viteBin, "build", "--config", "vite.config.mjs"],
        {
          cwd: APP,
        },
      );
      assert.equal(
        build.status,
        0,
        `a host bundling the packed package failed to build:\n${build.stdout}${build.stderr}`,
      );

      // ── Single React copy ────────────────────────────────────────────
      const recorded = JSON.parse(
        fs.readFileSync(path.join(APP, "react-modules.json"), "utf-8"),
      );
      assert.ok(
        recorded.some((id) => /\/react\/index\.js$/.test(id)),
        "the app bundle resolved react (the walk is real, not vacuous)",
      );
      // The install root of every react/react-dom module: the path up to and
      // including the `node_modules` directory that holds it. A second React
      // bundled by the package would appear as a second, different root.
      const root = (id) =>
        id.slice(0, id.lastIndexOf("/node_modules/") + "/node_modules".length);
      const roots = new Set(recorded.map(root));
      assert.equal(
        roots.size,
        1,
        `the app bundle contains more than one React copy, from roots: ${[...roots].join(", ")}`,
      );
      // Both React runtimes resolve to the one root, and the app bundle
      // consumed each: a duplicate embedded inside the package would be a
      // second `react/index.js`/`react-dom/index.js` under the package's dist.
      for (const entry of ["/react/index.js", "/react-dom/index.js"]) {
        const matches = recorded.filter((id) => id.endsWith(entry));
        assert.equal(
          matches.length,
          1,
          `expected exactly one ${entry} resolved by the app, found ${matches.length}`,
        );
      }
    });
  },
);
