/**
 * Track R (--native-fs) stub for services/repoVfsBridge.ts.
 *
 * Vite's conditional alias (webui/vite.config.ts) swaps every import of
 * services/repoVfsBridge for this module when the dist build is invoked with
 * VITE_SPROUT_NATIVE_FS=1 (scripts/build-webui-dist.mjs --native-fs). This
 * bridge copies cloned-repo working-tree files between lightning-fs (IndexedDB)
 * and the WASM VFS; with the FS provided natively there is no WASM VFS to
 * sync into, so the module is excluded from the bundle.
 *
 * No runtime dependency on the real module: the interfaces are re-exported
 * via `export type` (erased at compile time). The two value functions return
 * safe empty results rather than throw — a sync with nothing to write is a
 * valid no-op, and no caller in the app bundle invokes these in the
 * native-fs build (they have no runtime importers outside their own tests).
 */

import type { WasmWriter, SyncProgress } from '../repoVfsBridge';

// Re-export the type surface (type-only, erased at compile time).
export type { WasmWriter, SyncProgress } from '../repoVfsBridge';

/**
 * Stub for the real `syncRepoToWasmVfs`. Returns an empty result with a single
 * explanatory error so any accidental caller sees a clear signal instead of
 * silently "syncing" zero files.
 */
export async function syncRepoToWasmVfs(
  _repoDir: string,
  _wasmDir: string,
  _wasm: WasmWriter,
  _onProgress?: (p: SyncProgress) => void,
): Promise<{ copied: number; skipped: number; errors: string[] }> {
  return {
    copied: 0,
    skipped: 0,
    errors: [
      'syncRepoToWasmVfs: provided natively by the shell (Track R --native-fs); the WASM VFS bridge was excluded from this build.',
    ],
  };
}

/**
 * Stub for the real `syncWasmFileToRepo`. Safe no-op: no WASM VFS file exists
 * to copy back in the native-fs build.
 */
export async function syncWasmFileToRepo(
  _repoDir: string,
  _wasmPath: string,
  _content: string,
): Promise<void> {
  // no-op
}