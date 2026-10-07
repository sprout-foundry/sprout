import { defineConfig } from "vite";
import dts from "vite-plugin-dts";
import { resolve } from "path";

/**
 * Library build for `@sprout-foundry/workspace`.
 *
 * ESM only, code-split, with declaration files. The scaffold surfaces one
 * static entry (`src/index.js`); the heavy views are the entry points a host
 * loads when a space opens, so each becomes its own chunk instead of being
 * folded into the entry:
 *
 *   - `index` — the host contract and the space registry.
 *   - `views` — the views entry point (chat, agent changes, files, preview,
 *     the layout).
 *   - `design` — the design space's views.
 *
 * Declared as ad-hoc entries rather than a static re-export, because a static
 * re-export chains into one 8 MB chunk — the entry chunk is a fourth of
 * everything reachable from the entry point, so it cannot be told apart from
 * a second entry chunk by looking at what it pulls in.
 */
export default defineConfig({
  plugins: [
    dts({
      include: ["src/**/*"],
      outDirs: "dist",
      tsconfigPath: "./tsconfig.build.json",
    }),
  ],
  build: {
    lib: {
      entry: {
        index: resolve(__dirname, "src/index.ts"),
        views: resolve(__dirname, "src/viewsChunk.ts"),
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
      output: {
        manualChunks: {},
        chunkFileNames: "chunks/[name]-[hash].js",
        assetFileNames: "chunks/[name][extname]",
      },
    },
  },
});
