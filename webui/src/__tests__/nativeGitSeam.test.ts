// @vitest-environment node
/**
 * R-4: Track R (--native-git) vite alias seam tests. Mirrors
 * nativeChatSeam.test.ts (and the sibling nativeFs/nativeTerminal seam tests).
 *
 * Loads webui/vite.config.ts through Vite's own config loader
 * (`loadConfigFromFile` — fast, no bundle build) and asserts the
 * conditional `nativeGitStubAliases` seam:
 *   - default build            → exactly ONE alias entry (`@`), no git stubs
 *   - VITE_SPROUT_NATIVE_GIT=1
 *                             → 4 alias entries (3 git stub regexes + `@`),
 *                               git stub regexes ordered BEFORE `@`
 *
 * The git module lives at `services/api/gitApi` (NOT at the services root),
 * so the relative-form regex entries cover `./gitApi`, `./api/gitApi`, and
 * `../services/api/gitApi` (the `(?:../)+` form with an optional `services/`
 * segment). This test replicates the exact git-stub regex construction
 * (mirroring how nativeChatSeam asserts the chat ones) and verifies every
 * real import specifier of `services/api/gitApi` (the relative forms + the
 * @/ alias form, each with/without `.js`) matches the git stub alias regexes
 * when the flag is on — i.e. the bundle build would rewrite them to the stub.
 *
 * Also asserts a stub no-op proof: importing `services/nativeGitStubs/gitApi`
 * directly yields a no-op stand-in that NEVER fetches (mocked global fetch
 * stays uncalled) and resolves inert values matching the real return types.
 */

import { resolve as resolvePath } from 'node:path';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

/** Absolute path of this test file's directory (ESM node env has no __dirname). */
const here = fileURLToPath(new URL('.', import.meta.url));

// The git-stub import specifiers as they appear across the source tree
// (each may or may not carry a `.js` extension):
//   ../services/api/gitApi     (components/, hooks/ — the real importers)
//   ./gitApi                   (a sibling module inside services/api/)
//   ./api/gitApi               (a sibling module inside services/)
//   @/services/api/gitApi      (the alias-form specifier)
const GIT_SPECIFIERS = [
  '../services/api/gitApi',
  './gitApi',
  './api/gitApi',
  '@/services/api/gitApi',
  '../services/api/gitApi.js',
  './gitApi.js',
  './api/gitApi.js',
  '@/services/api/gitApi.js',
  '../../services/api/gitApi',
];

// ---------------------------------------------------------------------------
// Vite alias seam (via loadConfigFromFile)
// ---------------------------------------------------------------------------

/**
 * Snapshot/restore the git + chat + terminal + fs env vars so env never leaks
 * between cases. The additive cases set sibling flags too, so all four are
 * captured and restored here (mirrors the chat seam's three-var version).
 */
let savedGitEnv: string | undefined;
let savedChatEnv: string | undefined;
let savedTerminalEnv: string | undefined;
let savedFsEnv: string | undefined;
beforeEach(() => {
  savedGitEnv = process.env.VITE_SPROUT_NATIVE_GIT;
  savedChatEnv = process.env.VITE_SPROUT_NATIVE_CHAT;
  savedTerminalEnv = process.env.VITE_SPROUT_NATIVE_TERMINAL;
  savedFsEnv = process.env.VITE_SPROUT_NATIVE_FS;
  delete process.env.VITE_SPROUT_NATIVE_GIT;
  delete process.env.VITE_SPROUT_NATIVE_CHAT;
  delete process.env.VITE_SPROUT_NATIVE_TERMINAL;
  delete process.env.VITE_SPROUT_NATIVE_FS;
});
afterEach(() => {
  if (savedGitEnv === undefined) delete process.env.VITE_SPROUT_NATIVE_GIT;
  else process.env.VITE_SPROUT_NATIVE_GIT = savedGitEnv;
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

/** Pull the git-stub aliases (the RegExp `find` entries targeting gitApi). */
function gitStubAliases(
  config: Awaited<ReturnType<typeof loadWebuiViteConfig>>,
): Array<{ find: RegExp; replacement: string }> {
  const aliases = (config.resolve?.alias ?? []) as Array<{ find: unknown; replacement: string }>;
  return aliases.filter(
    (a): a is { find: RegExp; replacement: string } =>
      a.find instanceof RegExp && a.replacement.includes('nativeGitStubs'),
  );
}

describe('vite.config.ts native-git alias seam', () => {
  it('default (no VITE_SPROUT_NATIVE_GIT): exactly one alias entry (@), no git stubs', async () => {
    expect(process.env.VITE_SPROUT_NATIVE_GIT).toBeUndefined();

    const config = await loadWebuiViteConfig();
    const aliases = (config.resolve?.alias ?? []) as Array<{ find: unknown; replacement: string }>;
    expect(aliases).toHaveLength(1);
    expect(aliases[0]).toEqual({ find: '@', replacement: expect.stringContaining('src') });
    // No git (or fs / terminal / chat) stub alias anywhere in the config.
    expect(JSON.stringify(aliases)).not.toContain('nativeGitStubs');
    expect(gitStubAliases(config)).toHaveLength(0);
  });

  it('VITE_SPROUT_NATIVE_GIT=1: 4 alias entries, 3 git-stub regexes ordered before @', async () => {
    process.env.VITE_SPROUT_NATIVE_GIT = '1';
    try {
      const config = await loadWebuiViteConfig();
      const aliases = (config.resolve?.alias ?? []) as Array<{ find: unknown; replacement: string }>;
      expect(aliases).toHaveLength(4);

      // The three git-stub aliases must be the first three entries, in
      // order (alias-form, sibling ./, relative ../), each pointing at the
      // nativeGitStubs dir.
      const stubs = gitStubAliases(config);
      expect(stubs).toHaveLength(3);
      for (let i = 0; i < 3; i++) {
        expect(stubs[i].find).toBeInstanceOf(RegExp);
        expect(String(stubs[i].find.source)).toContain('gitApi');
        expect(stubs[i].replacement).toContain('nativeGitStubs');
      }
      // Each stub alias precedes the `@` alias (most-specific wins first).
      for (let i = 0; i < 3; i++) {
        expect(aliases[i]).toBe(stubs[i]);
      }
      // The @ alias must be LAST.
      expect(aliases[3]).toEqual({ find: '@', replacement: expect.stringContaining('src') });
    } finally {
      delete process.env.VITE_SPROUT_NATIVE_GIT;
    }
  });

  it('additive: --native-fs --native-terminal --native-chat --native-git yields 13 alias entries (3 fs + 3 terminal + 3 chat + 3 git + @)', async () => {
    process.env.VITE_SPROUT_NATIVE_FS = '1';
    process.env.VITE_SPROUT_NATIVE_TERMINAL = '1';
    process.env.VITE_SPROUT_NATIVE_CHAT = '1';
    process.env.VITE_SPROUT_NATIVE_GIT = '1';
    try {
      const config = await loadWebuiViteConfig();
      const aliases = (config.resolve?.alias ?? []) as Array<{ find: unknown; replacement: string }>;
      expect(aliases).toHaveLength(13);
      // fs stubs (3) come first, then terminal stubs (3), then chat stubs
      // (3), then git stubs (3), then `@`. All four flag sets are active
      // together — the additive ordering holds.
      expect(aliases[0].replacement).toContain('nativeFsStubs');
      expect(aliases[1].replacement).toContain('nativeFsStubs');
      expect(aliases[2].replacement).toContain('nativeFsStubs');
      expect(aliases[3].replacement).toContain('nativeTerminalStubs');
      expect(aliases[4].replacement).toContain('nativeTerminalStubs');
      expect(aliases[5].replacement).toContain('nativeTerminalStubs');
      expect(aliases[6].replacement).toContain('nativeChatStubs');
      expect(aliases[7].replacement).toContain('nativeChatStubs');
      expect(aliases[8].replacement).toContain('nativeChatStubs');
      expect(aliases[9].replacement).toContain('nativeGitStubs');
      expect(aliases[10].replacement).toContain('nativeGitStubs');
      expect(aliases[11].replacement).toContain('nativeGitStubs');
      expect(aliases[12]).toEqual({ find: '@', replacement: expect.stringContaining('src') });
    } finally {
      delete process.env.VITE_SPROUT_NATIVE_FS;
      delete process.env.VITE_SPROUT_NATIVE_TERMINAL;
      delete process.env.VITE_SPROUT_NATIVE_CHAT;
      delete process.env.VITE_SPROUT_NATIVE_GIT;
    }
  });

  it('git stubs come AFTER chat stubs and BEFORE @ (fs + terminal + chat + git order)', async () => {
    // When all four are set, the 13-entry set is [3 fs, 3 terminal, 3 chat,
    // 3 git, @] — git must sit between chat and @.
    process.env.VITE_SPROUT_NATIVE_FS = '1';
    process.env.VITE_SPROUT_NATIVE_TERMINAL = '1';
    process.env.VITE_SPROUT_NATIVE_CHAT = '1';
    process.env.VITE_SPROUT_NATIVE_GIT = '1';
    try {
      const config = await loadWebuiViteConfig();
      const aliases = (config.resolve?.alias ?? []) as Array<{ find: unknown; replacement: string }>;
      expect(aliases).toHaveLength(13);
      expect(aliases[6].replacement).toContain('nativeChatStubs');
      expect(aliases[7].replacement).toContain('nativeChatStubs');
      expect(aliases[8].replacement).toContain('nativeChatStubs');
      expect(aliases[9].replacement).toContain('nativeGitStubs');
      expect(aliases[10].replacement).toContain('nativeGitStubs');
      expect(aliases[11].replacement).toContain('nativeGitStubs');
      expect(aliases[12]).toEqual({ find: '@', replacement: expect.stringContaining('src') });
    } finally {
      delete process.env.VITE_SPROUT_NATIVE_FS;
      delete process.env.VITE_SPROUT_NATIVE_TERMINAL;
      delete process.env.VITE_SPROUT_NATIVE_CHAT;
      delete process.env.VITE_SPROUT_NATIVE_GIT;
    }
  });
});

// ---------------------------------------------------------------------------
// Real import specifiers match the git stub alias regexes
// ---------------------------------------------------------------------------

describe('git-stub alias regexes match the real import specifiers', () => {
  // The three regexes the vite config builds when the flag is on, replicated
  // here exactly (the config does not export them). This mirrors how
  // nativeChatSeam asserts the chat regex shapes.
  function gitStubs(): Array<{ find: RegExp; replacement: string }> {
    return [
      { find: /^@\/services\/api\/(gitApi)(?:\.js)?$/, replacement: 'STUBDIR/$1' },
      { find: /^\.\/(api\/)?(gitApi)(?:\.js)?$/, replacement: 'STUBDIR/$2' },
      { find: /^(?:\.\.\/)+(?:services\/)?api\/(gitApi)(?:\.js)?$/, replacement: 'STUBDIR/$1' },
    ];
  }

  it('every real gitApi import specifier is rewritten by some stub regex', () => {
    const stubs = gitStubs();
    for (const spec of GIT_SPECIFIERS) {
      const matched = stubs.some((s) => s.find.test(spec));
      expect(matched, `specifier ${JSON.stringify(spec)} must be rewritten to the git stub`).toBe(true);
    }
  });

  it('a non-git specifier (e.g. chatApi, or another module) is NOT matched', () => {
    const stubs = gitStubs();
    // Close-but-wrong module names must NOT be captured.
    expect(stubs.some((s) => s.find.test('../services/api/chatApi'))).toBe(false);
    expect(stubs.some((s) => s.find.test('@/services/api/gitApiExtra'))).toBe(false); // wrong name
    expect(stubs.some((s) => s.find.test('../services/api/GitApi'))).toBe(false); // wrong case
    expect(stubs.some((s) => s.find.test('../services/websocket'))).toBe(false); // other module
  });

  it('the regex set is the SAME construction the vite config uses (flag-ON config agrees)', async () => {
    // Cross-check: the flag-ON config's actual git-stub regex sources match
    // the replicated regexes, so this suite isn't drifting from source.
    process.env.VITE_SPROUT_NATIVE_GIT = '1';
    try {
      const config = await loadWebuiViteConfig();
      const actual = gitStubAliases(config).map((s) => String(s.find.source));
      const expected = gitStubs().map((s) => String(s.find.source));
      expect(actual).toEqual(expected);
    } finally {
      delete process.env.VITE_SPROUT_NATIVE_GIT;
    }
  });
});

// ---------------------------------------------------------------------------
// Stub no-op proof (the hard-exclusion stand-in)
// ---------------------------------------------------------------------------

describe('nativeGitStubs/gitApi (inert no-op surface)', () => {
  it('importing the stub directly yields a module that NEVER fetches', async () => {
    // Spy on global fetch; the stub takes fetch as its first arg, so we pass
    // a spy and confirm it is never called by any stub function.
    const fetchSpy = vi.fn();
    const stub = await import('../services/nativeGitStubs/gitApi');

    // Exercise a representative spread of the 20 exported functions; each
    // resolves its inert value without touching fetch.
    const status = await stub.getGitStatus(fetchSpy);
    const branches = await stub.getGitBranches(fetchSpy);
    const checkout = await stub.checkoutGitBranch(fetchSpy, 'main');
    const created = await stub.createGitBranch(fetchSpy, 'feature');
    await stub.pullGit(fetchSpy);
    await stub.pushGit(fetchSpy);
    const staged = await stub.stageFile(fetchSpy, 'a.txt');
    const unstaged = await stub.unstageFile(fetchSpy, 'b.txt');
    const discarded = await stub.discardChanges(fetchSpy, 'c.txt');
    await stub.stageAll(fetchSpy);
    await stub.unstageAll(fetchSpy);
    const commit = await stub.createCommit(fetchSpy, 'msg', ['a.txt']);
    const msg = await stub.generateCommitMessage(fetchSpy);
    const log = await stub.getGitLog(fetchSpy, 10, 0);
    const detail = await stub.getGitCommitDetail(fetchSpy, 'abc123');
    const fileDiff = await stub.getGitCommitFileDiff(fetchSpy, 'abc123', 'a.txt');
    const commitCheckout = await stub.checkoutGitCommit(fetchSpy, 'abc123');
    const reverted = await stub.revertGitCommit(fetchSpy, 'abc123');
    const diff = await stub.getGitDiff(fetchSpy, 'a.txt');
    const pr = await stub.createPullRequest(fetchSpy, { title: 'T' });

    // The stub NEVER issues a fetch — not even once across all 21 calls.
    expect(fetchSpy).not.toHaveBeenCalled();

    // Inert return values match the real signatures.
    expect(status).toEqual({
      message: 'Git provided by the native shell',
      in_git_repo: false,
      status: {
        branch: '',
        ahead: 0,
        behind: 0,
        staged: [],
        modified: [],
        untracked: [],
        deleted: [],
        renamed: [],
        in_git_repo: false,
      },
      files: [],
    });
    expect(branches).toEqual({ message: 'Git provided by the native shell', current: '', branches: [] });
    expect(checkout).toEqual({ message: 'Git provided by the native shell', branch: 'main' });
    expect(created).toEqual({ message: 'Git provided by the native shell', branch: 'feature' });
    expect(staged).toEqual({ message: 'Git provided by the native shell', path: 'a.txt' });
    expect(unstaged).toEqual({ message: 'Git provided by the native shell', path: 'b.txt' });
    expect(discarded).toEqual({ message: 'Git provided by the native shell', path: 'c.txt' });
    expect(commit).toEqual({ message: 'Git provided by the native shell', commit: '' });
    expect(msg).toEqual({ message: 'Git provided by the native shell', commit_message: '' });
    expect(log).toEqual({
      message: 'Git provided by the native shell',
      commits: [],
      offset: 0,
      limit: 10,
      total: 0,
    });
    expect(detail).toEqual({
      message: 'Git provided by the native shell',
      hash: 'abc123',
      short_hash: '',
      author: '',
      date: '',
      subject: '',
      files: [],
      diff: '',
      stats: '',
    });
    expect(fileDiff).toEqual({
      message: 'Git provided by the native shell',
      hash: 'abc123',
      path: 'a.txt',
      diff: '',
    });
    expect(commitCheckout).toEqual({ message: 'Git provided by the native shell' });
    expect(reverted).toEqual({ message: 'Git provided by the native shell' });
    expect(diff).toEqual({
      message: 'Git provided by the native shell',
      path: 'a.txt',
      has_staged: false,
      has_unstaged: false,
      staged_diff: '',
      unstaged_diff: '',
      diff: '',
    });
    expect(pr).toEqual({
      success: false,
      url: '',
      number: 0,
      state: 'Git provided by the native shell',
    });
  });

  it('createPullRequest returns an inert, not-success response (no network)', async () => {
    const stub = await import('../services/nativeGitStubs/gitApi');
    const pr = await stub.createPullRequest(vi.fn(), { title: 'My PR' });
    expect(pr.success).toBe(false);
    expect(pr.url).toBe('');
    expect(pr.number).toBe(0);
    expect(pr.state).toContain('native shell');
  });
});

// ---------------------------------------------------------------------------
// Stub type-compat proof: the stub mirrors the REAL gitApi surface
// ---------------------------------------------------------------------------
//
// The whole point of the stub is that a --native-git bundle (and `tsc
// --noEmit` under the flag) swaps the real services/api/gitApi for the stub
// with zero type errors. This test proves the surface stays in lockstep:
//   - every function the REAL module exports (extracted from its source)
//     exists on the stub module under the same name,
//   - and with the same parameter arity (real module's .length === stub's).
// If someone adds a new git API function to the real module, this test fails
// until the stub (and the alias regexes) are updated — the exact drift a
// type-only mirror can silently accumulate.

describe('nativeGitStubs/gitApi mirrors the real services/api/gitApi surface', () => {
  /** Function names exported by the REAL module (from its source). */
  function realGitApiExportNames(): string[] {
    const source = readFileSync(resolvePath(here, '../services/api/gitApi.ts'), 'utf-8');
    const names: string[] = [];
    // The real module exports only `export async function <name>(...)`.
    for (const m of source.matchAll(/^export async function (\w+)/gm)) {
      names.push(m[1]);
    }
    return names;
  }

  it('every real gitApi export exists on the stub under the same name', async () => {
    const stub = (await import('../services/nativeGitStubs/gitApi')) as unknown as Record<string, unknown>;
    const names = realGitApiExportNames();
    // Sanity: the extractor found the full surface (21 functions today).
    expect(names.length).toBeGreaterThanOrEqual(20);
    for (const name of names) {
      expect(name in stub, `stub is missing an export the real gitApi has: ${name}`).toBe(true);
      expect(typeof stub[name], `${name} on the stub must be a function`).toBe('function');
    }
    // And no EXTRA stub export that the real module lacks (the mirror must
    // be exact, not a superset).
    const stubNames = Object.keys(stub).filter((k) => typeof stub[k] === 'function');
    for (const name of stubNames) {
      expect(names.includes(name), `stub exports a function the real gitApi does not have: ${name}`).toBe(true);
    }
  });

  it('each stub export has the same parameter arity as the real one', async () => {
    // Import the REAL module in the node env: it is a pure function module
    // (no side effects at import time — all work is per-call via fetchFn),
    // so loading it alongside the stub is safe and side-effect-free.
    const real = (await import('../services/api/gitApi')) as unknown as Record<string, unknown>;
    const stub = (await import('../services/nativeGitStubs/gitApi')) as unknown as Record<string, unknown>;
    for (const name of realGitApiExportNames()) {
      const realFn = real[name] as { length: number } | undefined;
      const stubFn = stub[name] as { length: number } | undefined;
      expect(realFn, `real export ${name}`).toBeDefined();
      expect(stubFn, `stub export ${name}`).toBeDefined();
      // Function.length = count of parameters before the first default/
      // rest (both modules write their signatures in the same style:
      // required leading params, then optional ones), so arities must match.
      expect((stubFn as unknown as { length: number }).length, `${name}: stub arity`).toBe(
        (realFn as unknown as { length: number }).length,
      );
    }
  });
});
