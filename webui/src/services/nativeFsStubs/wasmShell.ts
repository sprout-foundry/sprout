/**
 * Track R (--native-fs) stub for services/wasmShell.ts.
 *
 * Vite's conditional alias (webui/vite.config.ts) swaps every import of
 * services/wasmShell for this module when the dist build is invoked with
 * VITE_SPROUT_NATIVE_FS=1 (scripts/build-webui-dist.mjs --native-fs). The
 * native shell provides the POSIX shell / VFS natively, so the WASM loader
 * (Go→WASM binary fetch + IndexedDB + ONNX bridges) is hard-excluded from
 * the bundle.
 *
 * No runtime dependency on the real module: every interface is re-exported
 * via `export type` (erased at compile time, so it pulls no real code into
 * the bundle). The two value exports are stub implementations.
 */

import type {
  WasmShell,
  WasmShellResult,
  WasmCompletionResult,
  WasmDirEntry,
  WasmListDirResult,
  WasmReadFileResult,
  WasmChangeDirResult,
  SproutStore,
  SproutWasmAPI,
} from '../wasmShell';

// Re-export every named export of the real module so importers keep working.
// Type-only: erased by esbuild, so this creates NO runtime dependency.
export type {
  WasmShell,
  WasmShellResult,
  WasmCompletionResult,
  WasmDirEntry,
  WasmListDirResult,
  WasmReadFileResult,
  WasmChangeDirResult,
  SproutStore,
  SproutWasmAPI,
} from '../wasmShell';

function notProvidedNative(name: string): Error {
  return new Error(
    `wasmShell.${name}: provided natively by the shell (Track R --native-fs); ` +
      `the WASM FS module was excluded from this build.`,
  );
}

/**
 * Stub for the real `initWasmShell`. At call time it throws so any code path
 * that still tries to boot the WASM shell fails fast and loudly rather than
 * silently no-op'ing.
 */
export async function initWasmShell(_config?: {
  home?: string;
  wasmUrl?: string;
  wasmExecUrl?: string;
}): Promise<WasmShell> {
  throw notProvidedNative('initWasmShell');
}

/**
 * Stub for the real `resetWasmShell`. There is no singleton to reset when the
 * FS is provided natively, so this is a safe no-op.
 */
export function resetWasmShell(): void {
  // no-op: nothing to reset in the native-fs build.
}
