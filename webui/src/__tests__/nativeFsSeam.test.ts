// @vitest-environment node
/**
 * Deliverable 4: Track R (--native-fs) vite alias seam tests.
 *
 * Loads webui/vite.config.ts through Vite's own config loader
 * (`loadConfigFromFile` — fast, no bundle build) and asserts the
 * conditional `nativeFsStubAliases` seam:
 *   - default build  → exactly ONE alias entry (`@`), no stub aliases
 *   - VITE_SPROUT_NATIVE_FS=1 → 4 entries, 3 stub regexes BEFORE `@`
 * Also sanity-checks the stub modules themselves (the hard-exclusion
 * stand-ins in src/services/nativeFsStubs/).
 */

import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { existsSync } from 'node:fs';
import { resolve as resolvePath } from 'node:path';
import { fileURLToPath } from 'node:url';

/**
 * Absolute path of this test file's directory. `__dirname` is not defined
 * in the ESM node environment, so anchor on import.meta.url (robust to cwd).
 */
const here = fileURLToPath(new URL('.', import.meta.url));

// ---------------------------------------------------------------------------
// Vite alias seam (via loadConfigFromFile)
// ---------------------------------------------------------------------------

/** Snapshot/restore VITE_SPROUT_NATIVE_FS so env never leaks between cases. */
let savedNativeFsEnv: string | undefined;
beforeEach(() => {
  savedNativeFsEnv = process.env.VITE_SPROUT_NATIVE_FS;
  delete process.env.VITE_SPROUT_NATIVE_FS;
});
afterEach(() => {
  if (savedNativeFsEnv === undefined) {
    delete process.env.VITE_SPROUT_NATIVE_FS;
  } else {
    process.env.VITE_SPROUT_NATIVE_FS = savedNativeFsEnv;
  }
});

async function loadWebuiViteConfig() {
  // Dynamic import of the bundled vite: keeps the test file CJS/ESM-agnostic
  // and localizes loader failures to the call site.
  const vite = await import('vite');
  const configPath = resolvePath(here, '../../vite.config.ts');
  const loaded = await vite.loadConfigFromFile(
    { command: 'build', mode: 'development' },
    configPath,
  );
  if (!loaded) {
    throw new Error(`loadConfigFromFile returned null for ${configPath}`);
  }
  return loaded.config;
}

describe('vite.config.ts native-fs alias seam', () => {
  it('default (no VITE_SPROUT_NATIVE_FS): exactly one alias entry (@), no stubs', async () => {
    // Env is deleted in beforeEach — assert the key is genuinely absent.
    expect(process.env.VITE_SPROUT_NATIVE_FS).toBeUndefined();

    const config = await loadWebuiViteConfig();
    const aliases = (config.resolve?.alias ?? []) as Array<{
      find: unknown;
      replacement: string;
    }>;
    expect(aliases).toHaveLength(1);
    expect(aliases[0]).toEqual({ find: '@', replacement: expect.stringContaining('src') });
    // No stub alias anywhere in the config.
    expect(JSON.stringify(aliases)).not.toContain('nativeFsStubs');
  });

  it('VITE_SPROUT_NATIVE_FS=1: 4 alias entries, stub regexes ordered before @', async () => {
    process.env.VITE_SPROUT_NATIVE_FS = '1';
    try {
      const config = await loadWebuiViteConfig();
      const aliases = (config.resolve?.alias ?? []) as Array<{
        find: unknown;
        replacement: string;
      }>;
      expect(aliases).toHaveLength(4);

      // The three stub aliases must be the first three entries, in order.
      const stubPatterns = [
        'fileAccess',
        'repoVfsBridge',
        'opfsReplica',
        'wasmShell',
      ];
      for (let i = 0; i < 3; i++) {
        expect(aliases[i].find).toBeInstanceOf(RegExp);
        expect(String((aliases[i].find as RegExp).source)).toContain(stubPatterns[i]);
        expect(aliases[i].replacement).toContain('nativeFsStubs');
      }
      // The @ alias must be LAST (most-specific stubs win first).
      expect(aliases[3]).toEqual({ find: '@', replacement: expect.stringContaining('src') });
    } finally {
      delete process.env.VITE_SPROUT_NATIVE_FS;
    }
  });
});

// ---------------------------------------------------------------------------
// Stub modules (the hard-exclusion stand-ins)
// ---------------------------------------------------------------------------

describe('nativeFsStubs modules', () => {
  it('fileAccess stub rejects with a "provided natively by the shell" error', async () => {
    const fileAccess = await import('../services/nativeFsStubs/fileAccess');
    await expect(fileAccess.readFileWithConsent('/tmp/x')).rejects.toThrow(
      /provided natively by the shell/,
    );
    await expect(
      fileAccess.writeFileWithConsent('/tmp/x', 'data'),
    ).rejects.toThrow(/Track R --native-fs/);
  });

  it('nativeFsFlag exports NATIVE_FS_ENABLED as boolean false when the flag is unset', async () => {
    // VITE_SPROUT_NATIVE_FS is deleted in beforeEach (and vitest does not
    // define it), so the compile-time constant must evaluate to false.
    const { NATIVE_FS_ENABLED } = await import('../services/nativeFsStubs/nativeFsFlag');
    expect(typeof NATIVE_FS_ENABLED).toBe('boolean');
    expect(NATIVE_FS_ENABLED).toBe(false);
  });

  it('all four stub files exist on disk (the alias targets are real)', async () => {
    for (const name of ['fileAccess', 'repoVfsBridge', 'opfsReplica', 'wasmShell']) {
      const p = resolvePath(here, `../services/nativeFsStubs/${name}.ts`);
      expect(existsSync(p), `${name}.ts stub`).toBe(true);
    }
  });
});