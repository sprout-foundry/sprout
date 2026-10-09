// Roll the package's public types up into self-contained declaration files.
//
// The package publishes three entries, each its own subpath and chunk:
//
//   - `.`        — `src/index.ts`, the host contract (the `exports.types`
//                  `dist/index.d.ts`).
//   - `./views`  — `src/viewsChunk.ts`, the views + space registry + the
//                  provider wrapper (`dist/views.d.ts`).
//   - `./providers` — `src/providersChunk.ts`, the provider wrapper on its own
//                  (`dist/providers.d.ts`).
//
// Each source re-exports a graph that lives in the web UI tree
// (`webui/src/...`), so emitted naively its declarations keep
// `../../../webui/src/...` import specifiers out of the package, and a host's
// TypeScript cannot resolve them from `dist/` alone.
//
// This script removes that dependency by rolling each entry's reachable graph
// up with API Extractor (the `rollupTypes` engine `vite-plugin-dts` wraps; see
// SP-160 §160a/§160e). The graphs are pre-built as `.d.ts` inside one scratch
// tree whose rootDir contains every module, so API Extractor can follow and
// inline them — its own analyzer cannot cross a tsconfig rootDir into a
// sibling package.
//
// The outputs therefore define every exported symbol and carry NO path outside
// the package: no `../../webui/...`, no nonexistent
// `@sprout-foundry/workspace-webui` namespace. External bare specifiers that
// remain (`react`, the peer a host installs) are the only imports.
//
// The declaration trees are built in a scratch dir and never emitted under
// `dist/`, so every `.d.ts` the package ships is self-contained.
import { spawnSync } from "node:child_process";
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
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
 * The workspace packages the entries bundle (they are dev dependencies, not
 * peers, so a host never installs them), and their source entry points.
 */
const BUNDLED_SOURCES = {
  "@sprout/ui": "packages/ui/src/index.ts",
  "@sprout/events": "packages/events/src/index.ts",
};

/**
 * Non-peer third-party type dependencies the public surface exposes through
 * the bundled graphs: the space registry's `WorkspaceMode.icon` is a
 * `LucideIcon`, and the `@sprout/ui` `Editor` props (re-exported by the views
 * entry) reference `@codemirror/*` (`Extension`, `oneDarkHighlightStyle`) and,
 * transitively, `@lezer/*` and `style-mod`. A host does not install them, so
 * the rollups must INLINE their declarations rather than keep the imports.
 * API Extractor resolves each through the linked `node_modules` and trims the
 * inlined graph to what the entry actually reaches.
 */
const BUNDLED_TYPES = [
  "lucide-react",
  "@codemirror/state",
  "@codemirror/language",
  "@codemirror/theme-one-dark",
  "@lezer/common",
  "@lezer/highlight",
  "style-mod",
];

/**
 * One public entry: the source whose graph is rolled up, the scratch-tree
 * declaration tsc emits for it (the entry API Extractor reads), and the
 * `.d.ts` written under `dist/` that its `exports` subpath points at.
 */
const ENTRIES = [
  {
    subpath: ".",
    source: "src/index.ts",
    declaration: "packages/workspace/src/index.d.ts",
    outFile: "index.d.ts",
    required: [
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
    ],
  },
  {
    subpath: "./views",
    source: "src/viewsChunk.ts",
    declaration: "packages/workspace/src/viewsChunk.d.ts",
    outFile: "views.d.ts",
    required: [
      "SproutWorkspace",
      "SproutWorkspaceProps",
      "SproutProject",
      "SproutProviders",
      "SproutProvidersProps",
      "WORKSPACE_MODES",
      "availableModes",
      "registerWorkspaceMode",
      "resolveWorkspaceMode",
      "useWorkspaceMode",
      "WorkspaceMode",
      "WorkspaceModeId",
      "WorkspaceModeContext",
      "WorkspaceModeRegistration",
      "WorkspaceShellProps",
      "ViewsLayout",
      "ViewsArrangement",
      "ChatView",
      "AgentChangesPanel",
      "WorkspaceChatProvider",
      "WorkspaceChatProviderProps",
      "useWorkspaceChat",
      "useWorkspaceChatProps",
      "createEmptyChatState",
      "WorkspaceChatValue",
      "WorkspaceChatRefs",
      "WorkspaceChatViewProps",
      "WorkspaceChatPropsOverrides",
      "WorkspaceChatReviewProps",
      "WorkspaceChatDiffState",
    ],
  },
  {
    subpath: "./providers",
    source: "src/providersChunk.ts",
    declaration: "packages/workspace/src/providersChunk.d.ts",
    outFile: "providers.d.ts",
    required: ["SproutProviders", "SproutProvidersProps"],
  },
];

/**
 * Build the declaration tree for every entry into `outDir` in one tsc pass.
 *
 * Sources: the entry modules plus the graphs they re-export. The tsconfig is
 * generated fresh in `outDir` rather than reusing `tsconfig.build.json`:
 * webui's `.d.ts`-only include and its rootDir would either drop the graphs or
 * place them outside a rootDir API Extractor can descend.
 */
function emitDeclarations(outDir) {
  const sourceFiles = ENTRIES.map((entry) => resolve(packageDir, entry.source));
  const hostIndex = resolve(repoRoot, "webui/src/host/index.ts");
  if (!existsSync(hostIndex)) {
    fail(`the host graph is missing: ${hostIndex}`);
  }
  sourceFiles.push(hostIndex);

  // The graphs read ambient web UI globals (`import.meta.env`,
  // `window.sproutDesktop`, `window.SPROUT_PROXY_BASE`) declared in these
  // `.d.ts` files. tsc does not follow them through the import graph, so they
  // are added to the program explicitly — without them every module reading a
  // global fails to compile. They are ambient (no runtime code) and emit no
  // declaration that reaches the rollup.
  for (const ambient of [
    "webui/src/vite-env.d.ts",
    "webui/src/custom.d.ts",
    "webui/src/types/desktop-api.d.ts",
  ]) {
    const file = resolve(repoRoot, ambient);
    if (!existsSync(file))
      fail(`a web UI ambient declaration is missing: ${file}`);
    sourceFiles.push(file);
  }

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
            // The graphs reach `@sprout/ui` and `@sprout/events` (bundled dev
            // dependencies a host does not install). Map the specifiers to the
            // packages' sources so tsc emits their declarations into the
            // scratch graph and API Extractor INLINES them, keeping the rollups
            // free of a bare `@sprout/*` import a host could not resolve.
            ...Object.fromEntries(
              Object.entries(BUNDLED_SOURCES).map(([specifier, source]) => [
                specifier,
                [resolve(repoRoot, source)],
              ]),
            ),
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

/** Every `.d.ts` under `dir`, recursively. */
function collectDts(dir, acc = []) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = resolve(dir, entry.name);
    if (entry.isDirectory()) collectDts(full, acc);
    else if (entry.name.endsWith(".d.ts")) acc.push(full);
  }
  return acc;
}

/**
 * Load API Extractor through the package's own resolution chain (a
 * devDependency), not a global install.
 */
function loadApiExtractor() {
  const require = createRequire(packageJsonPath);
  try {
    return require("@microsoft/api-extractor");
  } catch {
    fail(
      "@microsoft/api-extractor is not installed; run `npm install` in the package",
    );
  }
}

/**
 * Write the scratch scaffolding API Extractor needs once for the whole run:
 * a package.json in its projectFolder (to resolve the working package), the
 * tsconfig whose program is exactly the emitted `.d.ts` tree, and the config
 * that rolls a given entry up.
 */
function writeScratchScaffolding(outDir) {
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

  // API Extractor inlines `bundledPackages` by resolving each one through the
  // node module resolution chain from `projectFolder`. The scratch dir has no
  // `node_modules`, so link the repo's own — the third-party type dependencies
  // the public surface exposes (`lucide-react`, `@codemirror/*`) then resolve
  // and can be inlined rather than left as imports a host cannot resolve. The
  // bundled workspace packages (`@sprout/ui`, `@sprout/events`) are mapped to
  // their scratch-emitted declarations via the tsconfig `paths` instead (their
  // node_modules copies are built dists, not the source graph).
  const scratchNodeModules = resolve(outDir, "node_modules");
  if (!existsSync(scratchNodeModules)) {
    symlinkSync(resolve(repoRoot, "node_modules"), scratchNodeModules, "dir");
  }

  // API Extractor's analyzer refuses a program whose source files are not all
  // `.d.ts` (`ae-wrong-input-file-type`): feeding it the emit tsconfig would
  // put the raw `.ts` roots in its program. Point it at a tsconfig whose
  // program is exactly the EMITTED `.d.ts` tree, so it analyzes compiler
  // outputs (what it is for) and can follow/inline the whole graph.
  const emittedDts = collectDts(outDir);
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
          // Node resolution (not `bundler`): API Extractor resolves the
          // `bundledPackages` third-party type dependencies through the linked
          // scratch `node_modules` to inline them.
          moduleResolution: "node",
          skipLibCheck: true,
          strict: true,
          target: "ES2020",
          rootDir: outDir,
          typeRoots: [resolve(repoRoot, "node_modules/@types")],
          types: ["node"],
          baseUrl: outDir,
          paths: {
            // Resolve the bundled `@sprout/*` types to the scratch-emitted
            // declarations so API Extractor treats them as part of the program
            // and INLINES their exported types — otherwise it would keep a bare
            // `@sprout/*` import a host (which does not install them) cannot
            // resolve. The bundled non-peer third-party types (`lucide-react`,
            // `@codemirror/*`) resolve through the linked `node_modules` and
            // are inlined via `bundledPackages` (below). `react` stays
            // external: it is a peer the host provides.
            ...Object.fromEntries(
              Object.entries(BUNDLED_SOURCES).map(([specifier, source]) => [
                specifier,
                [resolve(outDir, source.replace(/\.ts$/, ".d.ts"))],
              ]),
            ),
          },
        },
        files: emittedDts,
      },
      null,
      2,
    ),
  );
}

/**
 * Roll one entry's emitted graph up into a declaration file with API
 * Extractor, then return its contents.
 */
function rollupEntry(outDir, entry) {
  const { Extractor, ExtractorConfig } = loadApiExtractor();

  if (!existsSync(resolve(outDir, entry.declaration))) {
    fail(
      `tsc did not emit the declaration for ${entry.subpath}: ${entry.declaration}`,
    );
  }

  const rolledUp = join(
    outDir,
    `rollup-${entry.outFile.replace(/\.d\.ts$/, "")}.d.ts`,
  );
  const configPath = resolve(
    outDir,
    `api-extractor-${entry.outFile.replace(/\.d\.ts$/, "")}.json`,
  );
  writeFileSync(
    configPath,
    JSON.stringify(
      {
        projectFolder: outDir,
        compiler: { tsconfigFilePath: resolve(outDir, "tsconfig.ae.json") },
        mainEntryPointFilePath: resolve(outDir, entry.declaration),
        // The bundled packages (workspace packages plus the non-peer
        // third-party type dependencies the public surface exposes) are
        // inlined, not left as imports a host cannot resolve. `react` and
        // `react-dom` are peers and stay external.
        bundledPackages: [...Object.keys(BUNDLED_SOURCES), ...BUNDLED_TYPES],
        apiReport: { enabled: false },
        docModel: { enabled: false },
        tsdocMetadata: { enabled: false },
        dtsRollup: {
          enabled: true,
          publicTrimmedFilePath: rolledUp,
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
      `API Extractor could not roll ${entry.subpath} up:\n${errors.join("\n")}`,
    );
  }

  if (!existsSync(rolledUp)) {
    fail(`API Extractor produced no rollup declaration for ${entry.subpath}`);
  }
  return readFileSync(rolledUp, "utf-8");
}

/**
 * Each entry's public surface must define every symbol a host imports. A
 * rollup that silently dropped the surface (an empty file, a bad symbol) would
 * ship a stub, so the emitted file is checked before it is written.
 */
function assertPublicSurface(source, entry) {
  const missing = entry.required.filter(
    (symbol) => !new RegExp(`\\b${symbol}\\b`).test(source),
  );
  if (missing.length > 0) {
    fail(
      `the ${entry.subpath} declaration is missing public symbols: ${missing.join(", ")}`,
    );
  }
  // No path may leave the package, and the removed workspace-webui namespace
  // must never reappear.
  if (/(?:\.\.\/)+webui\/|@sprout-foundry\/workspace-webui/.test(source)) {
    fail(
      `the ${entry.subpath} declaration still references a path outside the package`,
    );
  }
  // The only bare import a host can resolve is a peer dependency (react).
  // A surviving `@sprout/ui`, `@codemirror/*` or any other non-peer import
  // would make the types unresolvable for a host that does not install it.
  for (const match of source.matchAll(/from\s*["']([^"'.][^"']*)["']/g)) {
    const specifier = match[1];
    if (!/^react(-dom)?(\/|$)/.test(specifier)) {
      fail(
        `the ${entry.subpath} declaration imports a non-peer package: ${specifier}`,
      );
    }
  }
}

const scratch = mkdtempSync(resolve(tmpdir(), "workspace-dts-"));
try {
  emitDeclarations(scratch);
  writeScratchScaffolding(scratch);

  mkdirSync(dist, { recursive: true });
  for (const entry of ENTRIES) {
    const rolledUp = rollupEntry(scratch, entry);
    assertPublicSurface(rolledUp, entry);
    writeFileSync(resolve(dist, entry.outFile), rolledUp);
  }
} finally {
  rmSync(scratch, { recursive: true, force: true });
}
