// Roll the package's public types up into ONE self-contained declaration file.
//
// The package's `.` entry is the host contract: `src/index.ts` re-exports the
// web UI's host tree (`webui/src/host/*`) plus the package's own version
// constants. Emitted naively, the declarations keep `../../../webui/src/...`
// import specifiers out of the package, so a host's TypeScript cannot resolve
// the package's types from `dist/` alone.
//
// This script removes that dependency by rolling the reachable host graph up
// with API Extractor (the `rollupTypes` engine `vite-plugin-dts` wraps; see
// SP-160 §160a/§160e). The graph is pre-built as `.d.ts` inside a scratch tree
// whose rootDir contains every module, so API Extractor can follow and inline
// the whole graph — its own analyzer cannot cross a tsconfig rootDir into a
// sibling package.
//
// The output `dist/index.d.ts` therefore defines every exported symbol
// (interfaces, the value/function declarations, the version constants) and
// carries NO path outside the package: no `../../webui/...`, no nonexistent
// `@sprout-foundry/workspace-webui` namespace. External bare specifiers that
// remain (`react`: a peer a host installs) are the only imports.
//
// The declaration tree is built in a scratch dir and never emitted under
// `dist/`, so every `.d.ts` the package ships is self-contained. The published
// `exports.types` entry is the only `.d.ts` under `dist/`.
import { spawnSync } from "node:child_process";
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";

const packageDir = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = resolve(packageDir, "../..");
const dist = resolve(packageDir, "dist");
const packageJsonPath = resolve(packageDir, "package.json");

const fail = (message) => {
  throw new Error(`bundle-dts: ${message}`);
};

/**
 * Build the declaration tree for the package's `.` entry into `outDir`.
 *
 * Sources: the package's own `src` (which re-exports the host tree) and the web
 * UI's `host` subtree. The tsconfig is generated fresh in `outDir` rather than
 * reusing `tsconfig.build.json`: webui's `.d.ts`-only include and its rootDir
 * would either drop the host graph or place it outside a rootDir API Extractor
 * can descend.
 */
function emitDeclarations(outDir) {
  const sourceFiles = [resolve(packageDir, "src/index.ts")];
  const hostIndex = resolve(repoRoot, "webui/src/host/index.ts");
  if (!existsSync(hostIndex)) {
    fail(`the host graph is missing: ${hostIndex}`);
  }
  sourceFiles.push(hostIndex);

  const scratchTsconfig = resolve(outDir, "tsconfig.rollup.json");
  writeFileSync(
    scratchTsconfig,
    JSON.stringify(
      {
        compilerOptions: {
          declaration: true,
          emitDeclarationOnly: true,
          declarationMap: false,
          sourceMap: false,
          esModuleInterop: true,
          forceConsistentCasingInFileNames: true,
          isolatedModules: true,
          jsx: "react-jsx",
          lib: ["DOM", "DOM.Iterable", "ES2020"],
          module: "ESNext",
          moduleResolution: "bundler",
          noEmit: false,
          resolveJsonModule: true,
          skipLibCheck: true,
          strict: true,
          target: "ES2020",
          outDir,
          rootDir: repoRoot,
          typeRoots: [resolve(repoRoot, "node_modules/@types")],
          types: ["node"],
          paths: {
            "vite/client": [resolve(repoRoot, "node_modules/vite/client.d.ts")],
            // The host contract's `HostNavigation.navItems` uses
            // `PlatformNavItem` from `@sprout/ui` (a bundled dev dependency a
            // host does not install). Map the specifier to the package's
            // source so tsc emits its declaration into the scratch graph and
            // API Extractor INLINES it, keeping `dist/index.d.ts` free of a
            // bare `@sprout/ui` import a host could not resolve.
            "@sprout/ui": [resolve(repoRoot, "packages/ui/src/index.ts")],
          },
          baseUrl: repoRoot,
        },
        files: sourceFiles,
      },
      null,
      2,
    ),
  );

  const tsc = resolve(repoRoot, "node_modules/.bin/tsc");
  const result = spawnSync(tsc, ["-p", scratchTsconfig], {
    cwd: repoRoot,
    encoding: "utf-8",
  });
  if (result.status !== 0) {
    fail(
      `tsc could not emit the declaration tree (exit ${result.status}):\n${result.stdout}${result.stderr}`,
    );
  }
}

/**
 * Roll the emitted graph up into one declaration file with API Extractor, then
 * return its contents. API Extractor is resolved through the package's own
 * resolution chain (a devDependency), not a global install.
 */
function rollupDeclarations(outDir) {
  const require = createRequire(packageJsonPath);
  let apiExtractor;
  try {
    apiExtractor = require("@microsoft/api-extractor");
  } catch {
    fail(
      "@microsoft/api-extractor is not installed; run `npm install` in the package",
    );
  }
  const { Extractor, ExtractorConfig } = apiExtractor;

  const entry = resolve(outDir, "packages/workspace/src/index.d.ts");
  if (!existsSync(entry)) {
    fail(`tsc did not emit the package entry declaration: ${entry}`);
  }

  // The rollup needs a package.json in its projectFolder (to resolve the
  // working package); a minimal one keeps the scratch dir self-identifying.
  writeFileSync(
    resolve(outDir, "package.json"),
    JSON.stringify({
      name: "@sprout-foundry/workspace-dts-rollup",
      version: "0.0.0",
      type: "module",
    }),
  );

  // API Extractor's analyzer refuses a program whose source files are not all
  // `.d.ts` (`ae-wrong-input-file-type`): feeding it the emit tsconfig would
  // put the raw `.ts` roots in its program. Point it at a tsconfig whose
  // program is exactly the EMITTED `.d.ts` tree, so it analyzes compiler
  // outputs (what it is for) and can follow/inline the whole graph.
  const emittedDts = [];
  const collectDts = (dir) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const full = resolve(dir, entry.name);
      if (entry.isDirectory()) collectDts(full);
      else if (entry.name.endsWith(".d.ts")) emittedDts.push(full);
    }
  };
  collectDts(outDir);
  if (emittedDts.length === 0) {
    fail("tsc emitted no declarations to roll up");
  }
  writeFileSync(
    resolve(outDir, "tsconfig.ae.json"),
    JSON.stringify(
      {
        compilerOptions: {
          esModuleInterop: true,
          jsx: "react-jsx",
          lib: ["DOM", "DOM.Iterable", "ES2020"],
          module: "ESNext",
          moduleResolution: "bundler",
          skipLibCheck: true,
          strict: true,
          target: "ES2020",
          rootDir: outDir,
          typeRoots: [resolve(repoRoot, "node_modules/@types")],
          types: ["node"],
          baseUrl: outDir,
          paths: {
            // Resolve the bundled `@sprout/ui` type to the scratch-emitted
            // declaration so API Extractor treats it as part of the program
            // and INLINES `PlatformNavItem` — otherwise it would keep a bare
            // `@sprout/ui` import a host (which does not install it) cannot
            // resolve. `react` stays external: it is a peer the host provides.
            "@sprout/ui": [resolve(outDir, "packages/ui/src/index.d.ts")],
          },
        },
        files: emittedDts,
      },
      null,
      2,
    ),
  );

  const configPath = resolve(outDir, "api-extractor.json");
  writeFileSync(
    configPath,
    JSON.stringify(
      {
        projectFolder: outDir,
        compiler: { tsconfigFilePath: resolve(outDir, "tsconfig.ae.json") },
        mainEntryPointFilePath: entry,
        apiReport: { enabled: false },
        docModel: { enabled: false },
        tsdocMetadata: { enabled: false },
        dtsRollup: {
          enabled: true,
          publicTrimmedFilePath: resolve(outDir, "index.d.ts"),
        },
        messages: {
          extractorMessageReporting: {
            // The declarations are a scaffold over webui sources whose TSDoc
            // does not carry release tags; the API report is off, so these are
            // advisory and must not fail the build.
            "ae-missing-release-tag": { logLevel: "none" },
            "ae-forgotten-export": { logLevel: "none" },
          },
        },
      },
      null,
      2,
    ),
  );

  const errors = [];
  const config = ExtractorConfig.loadFileAndPrepare(configPath);
  const result = Extractor.invoke(config, {
    localBuild: true,
    showVerboseMessages: false,
    messageCallback: (message) => {
      if (message.logLevel === "error") errors.push(message.text);
    },
  });
  if (!result.succeeded || errors.length > 0) {
    fail(
      `API Extractor could not roll the declarations up:\n${errors.join("\n")}`,
    );
  }

  const rolledUp = resolve(outDir, "index.d.ts");
  if (!existsSync(rolledUp)) {
    fail("API Extractor produced no rollup declaration file");
  }
  return readFileSync(rolledUp, "utf-8");
}

/**
 * The package's public surface is the host contract plus the version
 * constants; the rollup must still define every symbol a host imports. A
 * rollup that silently dropped the surface (an empty file, a bad symbol)
 * would ship a stub, so the emitted file is checked before it is written.
 */
function assertPublicSurface(source, outFile) {
  const required = [
    "SproutHost",
    "HostProvider",
    "HostContext",
    "headlessHost",
    "localHost",
    "defaultHost",
    "useHost",
    "useHostCapabilities",
    "getActiveHost",
    "setActiveHost",
    "upsertActiveHostCapabilities",
    "HostNotificationCount",
    "outwardURL",
    "repoSlug",
    "repoName",
    "githubRepoSlug",
    "WORKSPACE_PACKAGE_NAME",
    "WORKSPACE_PACKAGE_VERSION",
  ];
  const missing = required.filter(
    (symbol) => !new RegExp(`\\b${symbol}\\b`).test(source),
  );
  if (missing.length > 0) {
    fail(
      `the rolled-up declaration is missing public symbols: ${missing.join(", ")}`,
    );
  }
  // No path may leave the package, and the removed workspace-webui namespace
  // must never reappear.
  if (/(?:\.\.\/)+webui\/|@sprout-foundry\/workspace-webui/.test(source)) {
    fail(
      `the rolled-up declaration still references a path outside the package: ${outFile}`,
    );
  }
  // The only bare import a host can resolve is a peer dependency (react).
  // A surviving `@sprout/ui` (or any other non-peer) import would make the
  // types unresolvable for a host that does not install it.
  for (const match of source.matchAll(/from\s*["']([^"'.][^"']*)["']/g)) {
    const specifier = match[1];
    if (!/^react(-dom)?(\/|$)/.test(specifier)) {
      fail(
        `the rolled-up declaration imports a non-peer package: ${specifier}`,
      );
    }
  }
}

const scratch = mkdtempSync(resolve(tmpdir(), "workspace-dts-"));
try {
  emitDeclarations(scratch);
  const rolledUp = rollupDeclarations(scratch);
  assertPublicSurface(rolledUp, resolve(dist, "index.d.ts"));

  mkdirSync(dist, { recursive: true });
  writeFileSync(resolve(dist, "index.d.ts"), rolledUp);
} finally {
  rmSync(scratch, { recursive: true, force: true });
}
