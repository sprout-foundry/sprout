import { build, defineConfig, type InlineConfig, type Plugin } from "vite";
import { resolve } from "path";

/**
 * Library build for `@sprout-foundry/workspace`.
 *
 * ESM only, code-split. The type declarations are rolled up separately by
 * `scripts/bundle-dts.mjs` (API Extractor), which emits the single
 * self-contained `dist/index.d.ts`; the Vite build itself emits no `.d.ts`
 * (a per-module emit would keep `../../../webui/src/...` import specifiers
 * out of the package, which a host cannot resolve).
 *
 *   - `index` — the host contract and the space registry.
 *   - `views` — the views entry point (chat, agent changes, files, preview,
 *     the layout).
 *   - `providers` — the provider wrapper the views need (`SproutProviders`).
 *   - `design` — the design space's views.
 *
 * Declared as ad-hoc entries rather than a static re-export, because a static
 * re-export chains into one 8 MB chunk — the entry chunk is a fourth of
 * everything reachable from the entry point, so it cannot be told apart from
 * a second entry chunk by looking at what it pulls in.
 */
const libraryConfig: InlineConfig = {
  root: __dirname,
  plugins: [],
  resolve: {
    // The workspace bundles `@sprout/ui` (its host modules import the
    // notification bus from it). Resolve the package to its source barrel,
    // not its pre-built `dist/index.esm.js`: a built barrel is a monolith
    // Rollup cannot tree-shake, so importing the bus would drag the whole
    // component graph — the editor included — into the package entry. From
    // source, only the modules the entry actually reaches are bundled; the
    // heavy view graph stays in the lazily loaded chunks.
    alias: [
      {
        find: /^@sprout\/ui$/,
        replacement: resolve(__dirname, "../ui/src/index.ts"),
      },
      {
        find: /^@sprout\/ui\/(.*)$/,
        replacement: resolve(__dirname, "../ui/src/$1"),
      },
    ],
  },
  build: {
    lib: {
      entry: {
        index: resolve(__dirname, "src/index.ts"),
        views: resolve(__dirname, "src/viewsChunk.ts"),
        providers: resolve(__dirname, "src/providersChunk.ts"),
        design: resolve(__dirname, "src/designChunk.ts"),
      },
      formats: ["es"],
      fileName: (_format, entryName) => `${entryName}.js`,
    },
    // Code-splitting is what keeps the heavy parts (the editor, the WASM
    // agent, the individual spaces) out of the initial chunk: the entry
    // stays small and everything a space needs loads when that space opens.
    // A library build disables chunking unless told otherwise. Chunks are
    // collected in dist/chunks/ so the publish allowlist stays a short,
    // checkable list (entry points + declarations + chunks + stylesheet).
    rollupOptions: {
      // React and React DOM are peer dependencies: the host provides them.
      // Bundling them would give the host a second React and break hooks.
      external: [/^react($|\/)/, /^react-dom($|\/)/],
      output: {
        manualChunks: {},
        chunkFileNames: "chunks/[name]-[hash].js",
        assetFileNames: "chunks/[name][extname]",
      },
    },
  },
};

/**
 * A module that is imported both statically and dynamically must not keep its
 * dynamic edge in the published chunks.
 *
 * Rollup cannot move such a module into a chunk of its own, so it lands in a
 * shared chunk and the dynamic import is emitted as a namespace accessor —
 * `await import("./shared.js").then((m) => m.i)`. That code runs as shipped,
 * but a host re-bundles `dist/` (the documented install path), and Vite's
 * dynamic-import tree-shaking rewrites `const { x } = await import(...)` as if
 * `x` were a direct export of the chunk, ignoring the trailing `.then(...)`.
 * The rewritten import resolves to `undefined` and every destructure from it
 * throws at runtime (the agent event dispatcher, the repo restore).
 *
 * The dynamic edge buys no laziness for these modules anyway — a static path
 * already loads them — so the library build turns each such `import(...)` into
 * a static namespace import and resolves the promise to it. The application
 * build is untouched: it is bundled once, by the same Rollup that splits it.
 *
 * A module whose only static importer is a package entry point stays lazy: it
 * is the entry's own chunk (the design views behind `design`), so its dynamic
 * import already resolves to that chunk's namespace without an accessor.
 *
 * The importer/target pairs come from a first, analysis-only pass over the same
 * graph (aborted once the module graph is complete). `scripts/check-chunk-graph.mjs`
 * verifies the emitted chunks afterwards.
 */
type DynamicEdges = Map<string, Set<string>>;

const ANALYSIS_DONE = "workspace-chunk-graph-analysis-complete";

async function sharedDynamicImports(config: InlineConfig): Promise<DynamicEdges> {
  const edges: DynamicEdges = new Map();
  const collect: Plugin = {
    name: "workspace:collect-shared-dynamic-imports",
    buildEnd(error) {
      if (error) return;
      const entries = new Set<string>();
      for (const id of this.getModuleIds()) {
        if (this.getModuleInfo(id)?.isEntry) entries.add(id);
      }
      for (const id of this.getModuleIds()) {
        const info = this.getModuleInfo(id);
        if (!info || info.dynamicImporters.length === 0) continue;
        if (!info.importers.some((importer) => !entries.has(importer))) continue;
        for (const importer of info.dynamicImporters) {
          const targets = edges.get(importer) ?? new Set<string>();
          targets.add(id);
          edges.set(importer, targets);
        }
      }
      throw new Error(ANALYSIS_DONE);
    },
  };
  try {
    await build({
      ...config,
      configFile: false,
      logLevel: "silent",
      plugins: [...(config.plugins ?? []), collect],
      build: { ...config.build, write: false },
    });
  } catch (error) {
    if (!String((error as Error)?.message ?? error).includes(ANALYSIS_DONE)) throw error;
  }
  return edges;
}

interface AstNode {
  type: string;
  start: number;
  end: number;
  source?: AstNode & { value?: unknown };
  [key: string]: unknown;
}

function dynamicImportsIn(node: unknown, found: AstNode[]): AstNode[] {
  if (Array.isArray(node)) {
    for (const child of node) dynamicImportsIn(child, found);
  } else if (node && typeof node === "object") {
    const n = node as AstNode;
    if (n.type === "ImportExpression") found.push(n);
    for (const key in n) {
      if (key !== "type" && key !== "start" && key !== "end") dynamicImportsIn(n[key], found);
    }
  }
  return found;
}

function staticSharedDynamicImports(edges: DynamicEdges): Plugin {
  return {
    name: "workspace:static-shared-dynamic-imports",
    // After the TypeScript/JSX transform, so the code parses as plain JS.
    enforce: "post",
    async transform(code, id) {
      const targets = edges.get(id);
      if (!targets) return null;
      const replacements: { start: number; end: number; text: string }[] = [];
      const hoisted: string[] = [];
      for (const node of dynamicImportsIn(this.parse(code), [])) {
        const specifier = node.source?.type === "Literal" ? node.source.value : undefined;
        if (typeof specifier !== "string") continue;
        const resolved = await this.resolve(specifier, id);
        if (!resolved || !targets.has(resolved.id)) continue;
        const binding = `__sproutSharedModule${hoisted.length}`;
        hoisted.push(`import * as ${binding} from ${JSON.stringify(specifier)};`);
        replacements.push({ start: node.start, end: node.end, text: `Promise.resolve(${binding})` });
      }
      if (replacements.length === 0) return null;
      let out = code;
      for (const { start, end, text } of replacements.sort((a, b) => b.start - a.start)) {
        out = out.slice(0, start) + text + out.slice(end);
      }
      return { code: `${hoisted.join("\n")}\n${out}`, map: null };
    },
  };
}

export default defineConfig(async ({ command }) => {
  if (command !== "build") return libraryConfig;
  const edges = await sharedDynamicImports(libraryConfig);
  return {
    ...libraryConfig,
    plugins: [...(libraryConfig.plugins ?? []), staticSharedDynamicImports(edges)],
  };
});
