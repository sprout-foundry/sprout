// @vitest-environment node
/**
 * R-4: Track R (--native-chat) vite alias seam tests. Mirrors
 * nativeTerminalSeam.test.ts (and nativeChatGate's companion leaf tests).
 *
 * Loads webui/vite.config.ts through Vite's own config loader
 * (`loadConfigFromFile` — fast, no bundle build) and asserts the
 * conditional `nativeChatStubAliases` seam:
 *   - default build            → exactly ONE alias entry (`@`), no chat stubs
 *   - VITE_SPROUT_NATIVE_CHAT=1
 *                             → 4 alias entries (3 chat stub regexes + `@`),
 *                               chat stub regexes ordered BEFORE `@`
 *
 * The chat module lives at `services/api/chatApi` (NOT at the services root),
 * so the relative-form regex entries cover `./chatApi`, `./api/chatApi`, and
 * `../services/api/chatApi` (the `(?:../)+` form with an optional
 * `services/` segment). This test replicates the exact chat-stub regex
 * construction (mirroring how nativeTerminalSeam asserts the terminal ones)
 * and verifies every real import specifier of `services/api/chatApi`
 * (the relative forms + the @/ alias form, each with/without `.js`) matches
 * the chat stub alias regexes when the flag is on — i.e. the bundle build
 * would rewrite them to the stub.
 *
 * Also asserts a stub no-op proof: importing `services/nativeChatStubs/chatApi`
 * directly yields a no-op stand-in that NEVER fetches (mocked global fetch
 * stays uncalled) and resolves inert values matching the real return types.
 */

import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { resolve as resolvePath } from 'node:path';
import { fileURLToPath } from 'node:url';

/** Absolute path of this test file's directory (ESM node env has no __dirname). */
const here = fileURLToPath(new URL('.', import.meta.url));

// The chat-stub import specifiers as they appear across the source tree
// (each may or may not carry a `.js` extension):
//   ../services/api/chatApi    (components/, hooks/ — the real importers)
//   ./chatApi                  (a sibling module inside services/api/)
//   ./api/chatApi              (a sibling module inside services/)
//   @/services/api/chatApi     (the alias-form specifier)
const CHAT_SPECIFIERS = [
  '../services/api/chatApi',
  './chatApi',
  './api/chatApi',
  '@/services/api/chatApi',
  '../services/api/chatApi.js',
  './chatApi.js',
  './api/chatApi.js',
  '@/services/api/chatApi.js',
  '../../services/api/chatApi',
];

// ---------------------------------------------------------------------------
// Vite alias seam (via loadConfigFromFile)
// ---------------------------------------------------------------------------

/**
 * Snapshot/restore the chat + terminal + fs env vars so env never leaks
 * between cases. The additive cases set sibling flags too, so all three are
 * captured and restored here (mirrors the terminal seam's two-var version).
 */
let savedChatEnv: string | undefined;
let savedTerminalEnv: string | undefined;
let savedFsEnv: string | undefined;
beforeEach(() => {
  savedChatEnv = process.env.VITE_SPROUT_NATIVE_CHAT;
  savedTerminalEnv = process.env.VITE_SPROUT_NATIVE_TERMINAL;
  savedFsEnv = process.env.VITE_SPROUT_NATIVE_FS;
  delete process.env.VITE_SPROUT_NATIVE_CHAT;
  delete process.env.VITE_SPROUT_NATIVE_TERMINAL;
  delete process.env.VITE_SPROUT_NATIVE_FS;
});
afterEach(() => {
  if (savedChatEnv === undefined) delete process.env.VITE_SPROUT_NATIVE_CHAT;
  else process.env.VITE_SPROUT_NATIVE_CHAT = savedChatEnv;
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

/** Pull the chat-stub aliases (the RegExp `find` entries targeting chatApi). */
function chatStubAliases(
  config: Awaited<ReturnType<typeof loadWebuiViteConfig>>,
): Array<{ find: RegExp; replacement: string }> {
  const aliases = (config.resolve?.alias ?? []) as Array<{ find: unknown; replacement: string }>;
  return aliases.filter(
    (a): a is { find: RegExp; replacement: string } =>
      a.find instanceof RegExp && a.replacement.includes('nativeChatStubs'),
  );
}

describe('vite.config.ts native-chat alias seam', () => {
  it('default (no VITE_SPROUT_NATIVE_CHAT): exactly one alias entry (@), no chat stubs', async () => {
    expect(process.env.VITE_SPROUT_NATIVE_CHAT).toBeUndefined();

    const config = await loadWebuiViteConfig();
    const aliases = (config.resolve?.alias ?? []) as Array<{ find: unknown; replacement: string }>;
    expect(aliases).toHaveLength(1);
    expect(aliases[0]).toEqual({ find: '@', replacement: expect.stringContaining('src') });
    // No chat (or terminal / fs) stub alias anywhere in the config.
    expect(JSON.stringify(aliases)).not.toContain('nativeChatStubs');
    expect(chatStubAliases(config)).toHaveLength(0);
  });

  it('VITE_SPROUT_NATIVE_CHAT=1: 4 alias entries, 3 chat-stub regexes ordered before @', async () => {
    process.env.VITE_SPROUT_NATIVE_CHAT = '1';
    try {
      const config = await loadWebuiViteConfig();
      const aliases = (config.resolve?.alias ?? []) as Array<{ find: unknown; replacement: string }>;
      expect(aliases).toHaveLength(4);

      // The three chat-stub aliases must be the first three entries, in
      // order (alias-form, sibling ./, relative ../), each pointing at the
      // nativeChatStubs dir.
      const stubs = chatStubAliases(config);
      expect(stubs).toHaveLength(3);
      for (let i = 0; i < 3; i++) {
        expect(stubs[i].find).toBeInstanceOf(RegExp);
        expect(String(stubs[i].find.source)).toContain('chatApi');
        expect(stubs[i].replacement).toContain('nativeChatStubs');
      }
      // Each stub alias precedes the `@` alias (most-specific wins first).
      for (let i = 0; i < 3; i++) {
        expect(aliases[i]).toBe(stubs[i]);
      }
      // The @ alias must be LAST.
      expect(aliases[3]).toEqual({ find: '@', replacement: expect.stringContaining('src') });
    } finally {
      delete process.env.VITE_SPROUT_NATIVE_CHAT;
    }
  });

  it('additive: --native-fs --native-terminal --native-chat yields 10 alias entries (3 fs + 3 terminal + 3 chat + @)', async () => {
    process.env.VITE_SPROUT_NATIVE_FS = '1';
    process.env.VITE_SPROUT_NATIVE_TERMINAL = '1';
    process.env.VITE_SPROUT_NATIVE_CHAT = '1';
    try {
      const config = await loadWebuiViteConfig();
      const aliases = (config.resolve?.alias ?? []) as Array<{ find: unknown; replacement: string }>;
      expect(aliases).toHaveLength(10);
      // fs stubs (3) come first, then terminal stubs (3), then chat stubs
      // (3), then `@`.
      expect(aliases[0].replacement).toContain('nativeFsStubs');
      expect(aliases[1].replacement).toContain('nativeFsStubs');
      expect(aliases[2].replacement).toContain('nativeFsStubs');
      expect(aliases[3].replacement).toContain('nativeTerminalStubs');
      expect(aliases[4].replacement).toContain('nativeTerminalStubs');
      expect(aliases[5].replacement).toContain('nativeTerminalStubs');
      expect(aliases[6].replacement).toContain('nativeChatStubs');
      expect(aliases[7].replacement).toContain('nativeChatStubs');
      expect(aliases[8].replacement).toContain('nativeChatStubs');
      expect(aliases[9]).toEqual({ find: '@', replacement: expect.stringContaining('src') });
    } finally {
      delete process.env.VITE_SPROUT_NATIVE_FS;
      delete process.env.VITE_SPROUT_NATIVE_TERMINAL;
      delete process.env.VITE_SPROUT_NATIVE_CHAT;
    }
  });

  it('chat stubs come AFTER terminal stubs and BEFORE @ (fs + terminal + chat order)', async () => {
    // When only terminal + chat are set (no fs), the 7-entry set is
    // [3 terminal, 3 chat, @] — chat must sit between terminal and @.
    process.env.VITE_SPROUT_NATIVE_TERMINAL = '1';
    process.env.VITE_SPROUT_NATIVE_CHAT = '1';
    try {
      const config = await loadWebuiViteConfig();
      const aliases = (config.resolve?.alias ?? []) as Array<{ find: unknown; replacement: string }>;
      expect(aliases).toHaveLength(7);
      expect(aliases[0].replacement).toContain('nativeTerminalStubs');
      expect(aliases[1].replacement).toContain('nativeTerminalStubs');
      expect(aliases[2].replacement).toContain('nativeTerminalStubs');
      expect(aliases[3].replacement).toContain('nativeChatStubs');
      expect(aliases[4].replacement).toContain('nativeChatStubs');
      expect(aliases[5].replacement).toContain('nativeChatStubs');
      expect(aliases[6]).toEqual({ find: '@', replacement: expect.stringContaining('src') });
    } finally {
      delete process.env.VITE_SPROUT_NATIVE_TERMINAL;
      delete process.env.VITE_SPROUT_NATIVE_CHAT;
    }
  });
});

// ---------------------------------------------------------------------------
// Real import specifiers match the chat stub alias regexes
// ---------------------------------------------------------------------------

describe('chat-stub alias regexes match the real import specifiers', () => {
  // The three regexes the vite config builds when the flag is on, replicated
  // here exactly (the config does not export them). This mirrors how
  // nativeTerminalSeam asserts the terminal regex shapes.
  function chatStubs(): Array<{ find: RegExp; replacement: string }> {
    return [
      { find: /^@\/services\/api\/(chatApi)(?:\.js)?$/, replacement: 'STUBDIR/$1' },
      { find: /^\.\/(api\/)?(chatApi)(?:\.js)?$/, replacement: 'STUBDIR/$2' },
      { find: /^(?:\.\.\/)+(?:services\/)?api\/(chatApi)(?:\.js)?$/, replacement: 'STUBDIR/$1' },
    ];
  }

  it('every real chatApi import specifier is rewritten by some stub regex', () => {
    const stubs = chatStubs();
    for (const spec of CHAT_SPECIFIERS) {
      const matched = stubs.some((s) => s.find.test(spec));
      expect(matched, `specifier ${JSON.stringify(spec)} must be rewritten to the chat stub`).toBe(true);
    }
  });

  it('a non-chat specifier (e.g. chatSessions, or another module) is NOT matched', () => {
    const stubs = chatStubs();
    // Close-but-wrong module names must NOT be captured.
    expect(stubs.some((s) => s.find.test('../services/api/chatSessions'))).toBe(false);
    expect(stubs.some((s) => s.find.test('@/services/api/chatApiExtra'))).toBe(false); // wrong name
    expect(stubs.some((s) => s.find.test('../services/api/ChatApi'))).toBe(false); // wrong case
    expect(stubs.some((s) => s.find.test('../services/websocket'))).toBe(false); // other module
  });

  it('the regex set is the SAME construction the vite config uses (flag-ON config agrees)', async () => {
    // Cross-check: the flag-ON config's actual chat-stub regex sources match
    // the replicated regexes, so this suite isn't drifting from source.
    process.env.VITE_SPROUT_NATIVE_CHAT = '1';
    try {
      const config = await loadWebuiViteConfig();
      const actual = chatStubAliases(config).map((s) => String(s.find.source));
      const expected = chatStubs().map((s) => String(s.find.source));
      expect(actual).toEqual(expected);
    } finally {
      delete process.env.VITE_SPROUT_NATIVE_CHAT;
    }
  });
});

// ---------------------------------------------------------------------------
// Stub no-op proof (the hard-exclusion stand-in)
// ---------------------------------------------------------------------------

describe('nativeChatStubs/chatApi (inert no-op surface)', () => {
  it('importing the stub directly yields a module that NEVER fetches', async () => {
    // Spy on global fetch; the stub takes fetch as its first arg, so we pass
    // a spy and confirm it is never called by any stub function.
    const fetchSpy = vi.fn();
    const stub = await import('../services/nativeChatStubs/chatApi');

    // Every function resolves its inert value without touching fetch.
    await stub.sendQuery(fetchSpy, 'hi', 'chat-1');
    const up = await stub.uploadImage(fetchSpy, new Blob(['x']) as unknown as File);
    await stub.steerQuery(fetchSpy, 'steer me', 'chat-1');
    const retract = await stub.retractSteer(fetchSpy, 'chat-1');
    const exec = await stub.executeCommand(fetchSpy, '/clear', 'chat-1');
    await stub.stopQuery(fetchSpy);
    const rewind = await stub.rewindQuery(fetchSpy, 2, true, 'chat-1');

    // The stub NEVER issues a fetch — not even once across all 7 calls.
    expect(fetchSpy).not.toHaveBeenCalled();

    // Inert return values match the real signatures.
    expect(up).toEqual({ path: '', filename: '' });
    expect(retract.success).toBe(true);
    expect(typeof retract.message).toBe('string');
    expect(exec.command).toBe('/clear');
    expect(exec.output).toBe('');
    expect(exec.accepted).toBe(false);
    expect(rewind).toEqual({
      turns_discarded: 0,
      messages_removed: 0,
      files_reverted: [],
      files_skipped: [],
      checkpoints_dropped: 0,
    });
  });

  it('executeCommand echoes the command but reports not-accepted + the native-shell error', async () => {
    const stub = await import('../services/nativeChatStubs/chatApi');
    const exec = await stub.executeCommand(vi.fn(), '/model gpt-4');
    expect(exec.command).toBe('/model gpt-4');
    expect(exec.accepted).toBe(false);
    expect(exec.output).toBe('');
    expect(exec.error).toContain('native shell');
  });
});