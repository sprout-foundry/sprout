// @vitest-environment node
/**
 * R-3: Track R (--native-terminal) vite alias seam tests. Mirrors
 * nativeFsSeam.test.ts.
 *
 * Loads webui/vite.config.ts through Vite's own config loader
 * (`loadConfigFromFile` — fast, no bundle build) and asserts the
 * conditional `nativeTerminalStubAliases` seam:
 *   - default build            → exactly ONE alias entry (`@`), no stub aliases
 *   - VITE_SPROUT_NATIVE_TERMINAL=1
 *                             → 4 alias entries (3 terminal stub regexes + `@`),
 *                               stub regexes ordered BEFORE `@`
 *
 * Because the config does NOT export the regexes, this test replicates the
 * exact terminal-stub regex construction (mirroring how nativeFsSeam asserts
 * the fs ones) and verifies every real import specifier of
 * `services/terminalWebSocket` (the three relative forms + the @/ alias form,
 * each with/without `.js`) matches the terminal stub alias regexes when the
 * flag is on — i.e. the bundle build would rewrite them to the stub.
 */

import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { resolve as resolvePath } from 'node:path';
import { fileURLToPath } from 'node:url';

/** Absolute path of this test file's directory (ESM node env has no __dirname). */
const here = fileURLToPath(new URL('.', import.meta.url));

// The terminal-stub import specifiers as they appear across the source tree
// (each may or may not carry a `.js` extension):
//   ../services/terminalWebSocket   (hooks/  — the real importers)
//   ./terminalWebSocket             (a sibling directory)
//   @/services/terminalWebSocket    (the alias-form specifier)
const TERMINAL_SPECIFIERS = [
  '../services/terminalWebSocket',
  './terminalWebSocket',
  '@/services/terminalWebSocket',
  '../services/terminalWebSocket.js',
  './terminalWebSocket.js',
  '@/services/terminalWebSocket.js',
];

// ---------------------------------------------------------------------------
// Vite alias seam (via loadConfigFromFile)
// ---------------------------------------------------------------------------

/** Snapshot/restore the terminal + fs env vars so env never leaks between cases. */
let savedTerminalEnv: string | undefined;
let savedFsEnv: string | undefined;
beforeEach(() => {
  savedTerminalEnv = process.env.VITE_SPROUT_NATIVE_TERMINAL;
  savedFsEnv = process.env.VITE_SPROUT_NATIVE_FS;
  delete process.env.VITE_SPROUT_NATIVE_TERMINAL;
  delete process.env.VITE_SPROUT_NATIVE_FS;
});
afterEach(() => {
  if (savedTerminalEnv === undefined) delete process.env.VITE_SPROUT_NATIVE_TERMINAL;
  else process.env.VITE_SPROUT_NATIVE_TERMINAL = savedTerminalEnv;
  if (savedFsEnv === undefined) delete process.env.VITE_SPROUT_NATIVE_FS;
  else process.env.VITE_SPROUT_NATIVE_FS = savedFsEnv;
});

async function loadWebuiViteConfig() {
  const vite = await import('vite');
  const configPath = resolvePath(here, '../../vite.config.ts');
  const loaded = await vite.loadConfigFromFile({ command: 'build', mode: 'development' }, configPath);
  if (!loaded) {
    throw new Error(`loadConfigFromFile returned null for ${configPath}`);
  }
  return loaded.config as { resolve?: { alias?: Array<{ find: unknown; replacement: string }> } };
}

/** Pull the terminal-stub aliases (the RegExp `find` entries targeting terminalWebSocket). */
function terminalStubAliases(
  config: Awaited<ReturnType<typeof loadWebuiViteConfig>>,
): Array<{ find: RegExp; replacement: string }> {
  const aliases = (config.resolve?.alias ?? []) as Array<{ find: unknown; replacement: string }>;
  return aliases.filter(
    (a): a is { find: RegExp; replacement: string } =>
      a.find instanceof RegExp && a.replacement.includes('nativeTerminalStubs'),
  );
}

describe('vite.config.ts native-terminal alias seam', () => {
  it('default (no VITE_SPROUT_NATIVE_TERMINAL): exactly one alias entry (@), no terminal stubs', async () => {
    expect(process.env.VITE_SPROUT_NATIVE_TERMINAL).toBeUndefined();

    const config = await loadWebuiViteConfig();
    const aliases = (config.resolve?.alias ?? []) as Array<{ find: unknown; replacement: string }>;
    expect(aliases).toHaveLength(1);
    expect(aliases[0]).toEqual({ find: '@', replacement: expect.stringContaining('src') });
    // No terminal (or fs) stub alias anywhere in the config.
    expect(JSON.stringify(aliases)).not.toContain('nativeTerminalStubs');
    expect(terminalStubAliases(config)).toHaveLength(0);
  });

  it('VITE_SPROUT_NATIVE_TERMINAL=1: 4 alias entries, 3 terminal-stub regexes ordered before @', async () => {
    process.env.VITE_SPROUT_NATIVE_TERMINAL = '1';
    try {
      const config = await loadWebuiViteConfig();
      const aliases = (config.resolve?.alias ?? []) as Array<{ find: unknown; replacement: string }>;
      expect(aliases).toHaveLength(4);

      // The three terminal-stub aliases must be the first three entries, in
      // order (alias-form, sibling ./, relative ../services/), each pointing
      // at the nativeTerminalStubs dir.
      const stubs = terminalStubAliases(config);
      expect(stubs).toHaveLength(3);
      for (let i = 0; i < 3; i++) {
        expect(stubs[i].find).toBeInstanceOf(RegExp);
        expect(String(stubs[i].find.source)).toContain('terminalWebSocket');
        expect(stubs[i].replacement).toContain('nativeTerminalStubs');
      }
      // Each stub alias precedes the `@` alias (most-specific wins first).
      for (let i = 0; i < 3; i++) {
        expect(aliases[i]).toBe(stubs[i]);
      }
      // The @ alias must be LAST.
      expect(aliases[3]).toEqual({ find: '@', replacement: expect.stringContaining('src') });
    } finally {
      delete process.env.VITE_SPROUT_NATIVE_TERMINAL;
    }
  });

  it('additive: --native-fs + --native-terminal yields 7 alias entries (3 fs + 3 terminal + @)', async () => {
    process.env.VITE_SPROUT_NATIVE_FS = '1';
    process.env.VITE_SPROUT_NATIVE_TERMINAL = '1';
    try {
      const config = await loadWebuiViteConfig();
      const aliases = (config.resolve?.alias ?? []) as Array<{ find: unknown; replacement: string }>;
      expect(aliases).toHaveLength(7);
      // fs stubs (3) come first, then terminal stubs (3), then `@`.
      expect(aliases[0].replacement).toContain('nativeFsStubs');
      expect(aliases[1].replacement).toContain('nativeFsStubs');
      expect(aliases[2].replacement).toContain('nativeFsStubs');
      expect(aliases[3].replacement).toContain('nativeTerminalStubs');
      expect(aliases[4].replacement).toContain('nativeTerminalStubs');
      expect(aliases[5].replacement).toContain('nativeTerminalStubs');
      expect(aliases[6]).toEqual({ find: '@', replacement: expect.stringContaining('src') });
    } finally {
      delete process.env.VITE_SPROUT_NATIVE_FS;
      delete process.env.VITE_SPROUT_NATIVE_TERMINAL;
    }
  });
});

// ---------------------------------------------------------------------------
// Real import specifiers match the terminal stub alias regexes
// ---------------------------------------------------------------------------

describe('terminal-stub alias regexes match the real import specifiers', () => {
  // The three regexes the vite config builds when the flag is on, replicated
  // here exactly (the config does not export them). This mirrors how
  // nativeFsSeam asserts the fs regex shapes.
  function terminalStubs(): Array<{ find: RegExp; replacement: string }> {
    return [
      { find: /^@\/services\/(terminalWebSocket)(?:\.js)?$/, replacement: 'STUBDIR/$1' },
      { find: /^\.\/(terminalWebSocket)(?:\.js)?$/, replacement: 'STUBDIR/$1' },
      { find: /^(?:\.\.\/)+services\/(terminalWebSocket)(?:\.js)?$/, replacement: 'STUBDIR/$1' },
    ];
  }

  it('every real terminalWebSocket import specifier is rewritten by some stub regex', () => {
    const stubs = terminalStubs();
    for (const spec of TERMINAL_SPECIFIERS) {
      const matched = stubs.some((s) => s.find.test(spec));
      expect(matched, `specifier ${JSON.stringify(spec)} must be rewritten to the terminal stub`).toBe(true);
    }
  });

  it('a non-terminal specifier (e.g. terminalWebSocketService, or fs module) is NOT matched', () => {
    const stubs = terminalStubs();
    // Close-but-wrong module names must NOT be captured.
    expect(stubs.some((s) => s.find.test('../services/terminalWebSocketService'))).toBe(false);
    expect(stubs.some((s) => s.find.test('@/services/terminalWebsocket'))).toBe(false); // wrong case
    expect(stubs.some((s) => s.find.test('../services/fileAccess'))).toBe(false); // fs module
  });

  it('the regex set is the SAME construction the vite config uses (flag-ON config agrees)', async () => {
    // Cross-check: the flag-ON config's actual terminal-stub regex sources
    // match the replicated regexes, so this suite isn't drifting from source.
    process.env.VITE_SPROUT_NATIVE_TERMINAL = '1';
    try {
      const config = await loadWebuiViteConfig();
      const actual = terminalStubAliases(config).map((s) => String(s.find.source));
      const expected = terminalStubs().map((s) => String(s.find.source));
      expect(actual).toEqual(expected);
    } finally {
      delete process.env.VITE_SPROUT_NATIVE_TERMINAL;
    }
  });
});
