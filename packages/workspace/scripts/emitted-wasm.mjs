// Resolve the content-hashed WASM assets the package emitted into dist/wasm/.
//
// The build (scripts/emit-wasm-assets.mjs) writes dist/wasm/ and its
// wasm-manifest.json; this is the small, testable reader that maps the manifest
// back to concrete filenames so a post-build step (e.g. the workspace-package
// artifact test) can assert the hashed names match the files on disk, and so a
// future tool can locate the emitted assets without hard-coding the hash.
import { existsSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

export const DIST_WASM_DIR = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "..",
  "dist",
  "wasm",
);

/** The fixed-name assets the package hashes, in the loader's manifest order. */
export const WASM_ASSETS = ["sprout.wasm", "wasm_exec.js"];

/**
 * Throw when any of the WASM sources is absent. The package cannot ship a
 * valid hosted artifact without them, so a missing source is a build failure
 * naming the target that produces it — never a silent skip that would emit an
 * empty dist/wasm/. Returns the source directory when all are present.
 */
export function assertWasmSourcesPresent(sourceDir, assets = WASM_ASSETS) {
  const missing = assets.filter((name) => !existsSync(join(sourceDir, name)));
  if (missing.length > 0) {
    throw new Error(
      `emit-wasm-assets: ${missing.join(", ")} missing from ${sourceDir}. ` +
        "Run `make build-wasm` (or `./scripts/build-wasm.sh`) before building the package.",
    );
  }
  return sourceDir;
}

/** Read dist/wasm/wasm-manifest.json, or null when the build has not run. */
export function readEmittedWasmManifest() {
  const manifestPath = join(DIST_WASM_DIR, "wasm-manifest.json");
  if (!existsSync(manifestPath)) return null;
  return JSON.parse(readFileSync(manifestPath, "utf-8"));
}

/** The emitted content-hashed filename for a logical asset (or null). */
export function emittedWasmFile(logicalName) {
  const manifest = readEmittedWasmManifest();
  return manifest?.files?.[logicalName] ?? null;
}
