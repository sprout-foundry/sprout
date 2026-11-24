/**
 * Backend resolution for the projectFolderFs seam.
 *
 * One call at startup: pick the native bridge backend when running inside
 * a studio shell (window.SproutStudioBridge present), the REST backend
 * when a daemon API is reachable (default on desktop/web), and fall back
 * to memory only when neither transport exists (tests, offline shell).
 *
 * Features never probe transports themselves — they ask for the seam:
 *
 *   const fs = await getProjectFolderFs();
 *   await fs.write('notes.md', 'hello');
 */

import { createMemoryFs } from './memoryFs';
import { createNativeBridgeFs, detectBridgeCall } from './nativeBridgeFs';
import { createRestFs } from './restFs';
import type { ProjectFolderFs } from './types';

let cached: ProjectFolderFs | null = null;

/**
 * Resolve the best available backend (cached after the first call).
 * Pass `{ force: 'memory' | 'native' | 'rest' }` to pin a backend in tests.
 */
export function getProjectFolderFs(opts: { force?: 'memory' | 'native' | 'rest' } = {}): ProjectFolderFs {
  if (opts.force) {
    switch (opts.force) {
      case 'native': {
        const call = detectBridgeCall();
        if (call) return createNativeBridgeFs(call);
        return createMemoryFs();
      }
      case 'rest':
        return createRestFs();
      case 'memory':
      default:
        return createMemoryFs();
    }
  }
  if (cached) return cached;
  const call = detectBridgeCall();
  cached = call ? createNativeBridgeFs(call) : createRestFs();
  return cached;
}

/** Test hook: drop the cached backend so the next call re-resolves. */
export function __resetProjectFolderFsForTests(): void {
  cached = null;
}

export { createMemoryFs, createNativeBridgeFs, createRestFs } from './backendsExport';
export type { ProjectFolderFs } from './types';
