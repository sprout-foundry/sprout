import { defineConfig } from "vite";
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
export default defineConfig({
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
});
